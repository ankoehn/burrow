#!/usr/bin/env bash
# test-only — never deploy this shape.
# test/harness/certs/gen.sh
# Regenerates the test CA + *.test.local wildcard cert pair. Idempotent.
# Requires openssl (any modern version).

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"

# On Git Bash / MSYS, leading-/ args (-subj "/C=US/...") get rewritten to
# Windows paths. Disable that conversion for openssl invocations below.
export MSYS_NO_PATHCONV=1

# 1. Root CA (10-year validity — these are test fixtures, never production).
# Only regenerate if the CA pair is absent; re-creating it would break the
# compose harness trust chain (BURROW_CERT_VALIDATION_ROOTS_FILE=ca.crt is
# baked into the running relay container).
if [[ ! -f ca.crt || ! -f ca.key ]]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
    -keyout ca.key -out ca.crt \
    -subj "/C=US/O=Burrow Test/CN=Burrow Test CA"
  echo "[gen.sh] created new test CA (ca.crt + ca.key)"
else
  echo "[gen.sh] reusing existing test CA (ca.crt + ca.key)"
fi

# 2. Wildcard leaf cert for *.test.local + test.local apex.
cat > wildcard.test.local.cnf <<'EOF'
[req]
default_bits       = 2048
prompt             = no
default_md         = sha256
req_extensions     = v3_req
distinguished_name = dn
[dn]
C  = US
O  = Burrow Test
CN = *.test.local
[v3_req]
keyUsage         = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName   = @alt
[alt]
DNS.1 = *.test.local
DNS.2 = test.local
DNS.3 = relay.test.local
DNS.4 = client-ai.test.local
DNS.5 = client-tcp.test.local
DNS.6 = client-multi.test.local
EOF

openssl req -new -newkey rsa:2048 -nodes \
  -keyout wildcard.test.local.key \
  -out wildcard.test.local.csr \
  -config wildcard.test.local.cnf

openssl x509 -req -in wildcard.test.local.csr -days 3650 \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out wildcard.test.local.crt \
  -extensions v3_req -extfile wildcard.test.local.cnf

# Cleanup CSR + serial (not committed).
rm -f wildcard.test.local.csr wildcard.test.local.cnf ca.srl
echo "[gen.sh] regenerated test CA + *.test.local cert (10-year validity)"

# 3. Client cert for mTLS e2e tests (spec 09 strengthened + spec 23).
# CN is opaque — Burrow's mTLS gate only verifies signature chain
# against the operator-supplied CA, not the CN.
openssl req -new -newkey rsa:2048 -nodes \
  -keyout client.key -out client.csr \
  -subj "/C=US/O=Burrow Test/CN=e2e-mtls-client"
openssl x509 -req -in client.csr -days 3650 \
  -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt \
  -extensions v3_req
rm -f client.csr ca.srl
echo "[gen.sh] regenerated client cert for mTLS tests"

# 4. Wildcard leaf cert for *.example.com + example.com apex.
#    Used by spec 31 to exercise the custom-domain proxy routing path.
cat > wildcard.example.com.cnf <<'EOF'
[req]
default_bits       = 2048
prompt             = no
default_md         = sha256
req_extensions     = v3_req
distinguished_name = dn
[dn]
C  = US
O  = Burrow Test
CN = *.example.com
[v3_req]
keyUsage         = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName   = @alt
[alt]
DNS.1 = *.example.com
DNS.2 = example.com
EOF

openssl req -new -newkey rsa:2048 -nodes \
  -keyout wildcard.example.com.key \
  -out wildcard.example.com.csr \
  -config wildcard.example.com.cnf

openssl x509 -req -in wildcard.example.com.csr -days 3650 \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out wildcard.example.com.crt \
  -extensions v3_req -extfile wildcard.example.com.cnf

# Cleanup CSR + serial + cnf (not committed).
rm -f wildcard.example.com.csr wildcard.example.com.cnf ca.srl
echo "[gen.sh] regenerated *.example.com cert for spec 31 custom-domain tests"
