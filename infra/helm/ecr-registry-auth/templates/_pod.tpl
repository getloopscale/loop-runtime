{{/* The pod both the install Job and the CronJob run: the AWS CLI mints the token into a
memory volume as a Secret manifest, and kubectl applies it. The token never reaches a log. */}}
{{- define "ecr-registry-auth.pod" -}}
serviceAccountName: loopruntime-registry-auth
restartPolicy: OnFailure
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 1000
  seccompProfile:
    type: RuntimeDefault
volumes:
  - name: work
    emptyDir:
      medium: Memory
      sizeLimit: 1Mi
initContainers:
  - name: token
    image: {{ required "images.awsCli is required" .Values.images.awsCli | quote }}
    command: ["sh", "-c"]
    args:
      - |
        set -eu
        aws ecr get-login-password --region "$AWS_REGION" | python3 -c '
        import base64, json, os, sys
        token = sys.stdin.read().strip()
        if not token:
            sys.exit("ecr-registry-auth: empty authorization token")
        auth = base64.b64encode(("AWS:" + token).encode()).decode()
        config = {"auths": {os.environ["REGISTRY_HOST"]: {"auth": auth}}}
        secret = {
            "apiVersion": "v1", "kind": "Secret", "type": "Opaque",
            "metadata": {"name": os.environ["SECRET_NAME"], "namespace": os.environ["NAMESPACE"]},
            "stringData": {"config.json": json.dumps(config)},
        }
        with open("/work/secret.json", "w") as out:
            json.dump(secret, out)
        '
        echo "ecr-registry-auth: token for $REGISTRY_HOST minted"
    env:
      - name: HOME
        value: /work
      - name: AWS_REGION
        value: {{ .Values.region | quote }}
      - name: REGISTRY_HOST
        value: {{ required "registryHost is required" .Values.registryHost | quote }}
      - name: SECRET_NAME
        value: {{ .Values.secretName | quote }}
      - name: NAMESPACE
        value: {{ .Release.Namespace | quote }}
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: ["ALL"]
    volumeMounts:
      - name: work
        mountPath: /work
    resources:
      requests: { cpu: 10m, memory: 64Mi }
      limits: { cpu: 500m, memory: 256Mi }
containers:
  - name: apply
    image: {{ .Values.images.kubectl | quote }}
    args: ["apply", "-f", "/work/secret.json"]
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: ["ALL"]
    volumeMounts:
      - name: work
        mountPath: /work
        readOnly: true
    resources:
      requests: { cpu: 10m, memory: 32Mi }
      limits: { cpu: 200m, memory: 128Mi }
{{- end }}
