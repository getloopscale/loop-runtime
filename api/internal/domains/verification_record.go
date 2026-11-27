package domains

// The TXT record a customer creates to prove they control a domain. It lives in their DNS, so
// its name and value carry the product's name. Every reader and writer of the record goes
// through these two functions, which keeps the name in one place.
const (
	verificationTXTLabel       = "_loopscale-verify"
	verificationTXTValuePrefix = "loopscale-verify="
)

// VerificationTXTName is the name of the TXT record that verifies domain.
func VerificationTXTName(domain string) string {
	return verificationTXTLabel + "." + domain
}

// VerificationTXTValue is the value the TXT record must hold for the given token.
func VerificationTXTValue(token string) string {
	return verificationTXTValuePrefix + token
}
