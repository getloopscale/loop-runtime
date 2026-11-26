// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sourcehttp

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/getloopscale/loop-runtime/api/internal/source"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	errorshttp "github.com/getloopscale/loop-runtime/pkg/errors/http"
)

// Wire field names of POST /api/v1/source/upload
// (docs/loopzerokit-loopzerocloud-seam.md § The flow).
const (
	fieldServicesJSON = "services_json"
	fieldBundle       = "bundle"
	fieldLegacyYAML   = "loopruntime_yaml"
)

// uploadRejection is a request the wire contract refuses. It carries the
// typed error the handler renders and, for a schema rejection, every field
// violation so the caller sees all of them at once (RFC 9457 § 3.2).
type uploadRejection struct {
	err    *loopruntimeerrors.LoopRuntimeError
	fields []errorshttp.FieldError
}

// uploadForm is a request that satisfied the wire contract: a validated
// manifest and the bundle spooled to a temp file positioned at offset 0.
// The caller owns the file and must call close.
type uploadForm struct {
	manifest *source.ServicesManifest
	bundle   *os.File
}

func (f *uploadForm) close() {
	_ = f.bundle.Close()
	_ = os.Remove(f.bundle.Name())
}

// readUploadForm streams the multipart body part by part and enforces the
// seam contract in the order it is written down:
//
//  1. the FIRST part is services_json (else 400 MULTIPART_FIELD_ORDER; a
//     legacy loopruntime_yaml part answers the more actionable E-009 instead);
//  2. services_json is at most maxServicesJSONSize (else 413);
//  3. services_json validates against the schema (else 422) — BEFORE a single
//     byte of the bundle is read, which is the reason the order exists;
//  4. a bundle part follows and is spooled to disk under the body cap.
//
// Streaming replaced a GetBody-based peek that net/http never enables on
// server requests, so the order was unenforced for every real upload.
func readUploadForm(r *http.Request, blocklist source.EnvBlocklistMode) (*uploadForm, *uploadRejection) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, &uploadRejection{err: loopruntimeerrors.ErrInvalidRequest}
	}

	first, err := mr.NextPart()
	if endOfParts(err) {
		return nil, &uploadRejection{err: missingServicesJSON()}
	}
	if err != nil {
		return nil, rejectionForReadError(err, loopruntimeerrors.ErrInvalidRequest)
	}
	manifest, rej := readManifestPart(first, blocklist)
	if rej != nil {
		return nil, rej
	}

	bundle, rej := spoolBundle(mr)
	if rej != nil {
		return nil, rej
	}
	return &uploadForm{manifest: manifest, bundle: bundle}, nil
}

// readManifestPart applies rules 1-3 to the first part.
func readManifestPart(part *multipart.Part, blocklist source.EnvBlocklistMode) (*source.ServicesManifest, *uploadRejection) {
	defer func() { _ = part.Close() }()

	switch name := part.FormName(); name {
	case fieldServicesJSON:
	case fieldLegacyYAML:
		return nil, &uploadRejection{err: loopruntimeerrors.NewLoopRuntimeError(
			"VALIDATION_FAILED",
			"E-009: legacy loopruntime_yaml not supported. This API version requires `.loopzero/services.json` emitted by LoopZeroKit ≥0.5.0. "+
				"Upgrade LoopZeroKit and re-run `loopzerokit build --target loopruntime-cloud` to regenerate the bundle.",
		)}
	default:
		return nil, &uploadRejection{err: &loopruntimeerrors.LoopRuntimeError{
			Code: "MULTIPART_FIELD_ORDER",
			Message: "multipart field order violation: expected `services_json` first, got `" + name +
				"`. Upgrade your LoopZeroKit CLI (≥0.5.0) — see https://loopruntimecloud.dev/docs/api/source-upload-wire-protocol",
		}}
	}

	raw, err := io.ReadAll(io.LimitReader(part, maxServicesJSONSize+1))
	if err != nil {
		return nil, rejectionForReadError(err, loopruntimeerrors.ErrInvalidRequest)
	}
	if int64(len(raw)) > maxServicesJSONSize {
		return nil, &uploadRejection{err: &loopruntimeerrors.LoopRuntimeError{
			Code: "SERVICES_JSON_TOO_LARGE",
			Message: fmt.Sprintf(
				"services.json field exceeds the %d KB cap. Reduce env var count / value length per `loopzerokit migrate services-json-v1-to-v2`.",
				maxServicesJSONSize/1024,
			),
		}}
	}
	if len(raw) == 0 {
		return nil, &uploadRejection{err: missingServicesJSON()}
	}

	manifest, err := source.ValidateServicesJSONNative(string(raw), blocklist)
	if err != nil {
		return nil, rejectionForManifestError(err)
	}
	return manifest, nil
}

// spoolBundle walks the remaining parts to the bundle and writes it to a temp
// file once, so the source store and the object store read from disk rather
// than from memory. Parts other than the bundle are drained and ignored, as
// the form parser this replaced ignored unknown fields.
func spoolBundle(mr *multipart.Reader) (*os.File, *uploadRejection) {
	for {
		part, err := mr.NextPart()
		if endOfParts(err) {
			return nil, &uploadRejection{err: loopruntimeerrors.NewLoopRuntimeError("MISSING_FIELD", "Bundle file is required")}
		}
		if err != nil {
			return nil, rejectionForReadError(err, loopruntimeerrors.ErrInvalidRequest)
		}
		if part.FormName() != fieldBundle {
			_, drainErr := io.Copy(io.Discard, part)
			_ = part.Close()
			if drainErr != nil {
				return nil, rejectionForReadError(drainErr, loopruntimeerrors.ErrInvalidRequest)
			}
			continue
		}
		file, rej := copyToTempFile(part)
		_ = part.Close()
		return file, rej
	}
}

func copyToTempFile(part io.Reader) (*os.File, *uploadRejection) {
	file, err := os.CreateTemp("", "loopruntime-bundle-*.tar.gz")
	if err != nil {
		slog.Error("source upload: failed to create temp file", "error", err)
		return nil, &uploadRejection{err: loopruntimeerrors.ErrInternalError}
	}
	discard := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	if _, err := io.Copy(file, part); err != nil {
		discard()
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &uploadRejection{err: loopruntimeerrors.ErrPayloadTooLarge}
		}
		slog.Error("source upload: failed to spool bundle", "error", err)
		return nil, &uploadRejection{err: loopruntimeerrors.ErrInvalidRequest}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		discard()
		slog.Error("source upload: failed to rewind spooled bundle", "error", err)
		return nil, &uploadRejection{err: loopruntimeerrors.ErrInternalError}
	}
	return file, nil
}

// endOfParts reports a well-formed multipart body with no further parts.
// multipart.Reader signals that with the BARE io.EOF; a body that is not
// multipart at all, or is cut off mid-stream, yields an io.EOF wrapped in
// "multipart: NextPart: …", which is a malformed request rather than a
// missing field.
func endOfParts(err error) bool {
	return errors.Is(err, io.EOF) && errors.Unwrap(err) == nil
}

// rejectionForReadError distinguishes a body over the size cap (413) from any
// other malformed stream, which gets the caller-chosen rejection.
func rejectionForReadError(err error, otherwise *loopruntimeerrors.LoopRuntimeError) *uploadRejection {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &uploadRejection{err: loopruntimeerrors.ErrPayloadTooLarge}
	}
	return &uploadRejection{err: otherwise}
}

// rejectionForManifestError maps a schema rejection to 422 carrying every
// field violation; anything else from the validator is an internal fault.
func rejectionForManifestError(err error) *uploadRejection {
	var ve *source.ValidationError
	if !errors.As(err, &ve) {
		slog.Error("source upload: services.json validator failed", "error", err)
		return &uploadRejection{err: loopruntimeerrors.ErrInternalError}
	}
	fields := make([]errorshttp.FieldError, 0, len(ve.Errors))
	for _, fe := range ve.Errors {
		fields = append(fields, errorshttp.FieldError{
			Field:    fe.Field,
			Message:  fe.Message,
			Expected: fe.Expected,
			Got:      fe.Got,
		})
	}
	return &uploadRejection{
		err:    loopruntimeerrors.NewLoopRuntimeError("VALIDATION_FAILED", ve.Message),
		fields: fields,
	}
}

func missingServicesJSON() *loopruntimeerrors.LoopRuntimeError {
	return loopruntimeerrors.NewLoopRuntimeError(
		"VALIDATION_FAILED",
		"E-008: services.json is required (multipart form field `services_json`).",
	)
}
