// Tiny inline wrapper over navigator.credentials.* — equivalent to the
// startRegistration/startAuthentication helpers from @simplewebauthn/browser
// (~4 KB gz) but with no new dependency. Handles base64url ↔ ArrayBuffer
// conversion and the IDL coercion the Web platform requires.

export interface BeginRegistrationOptions {
  publicKey: {
    rp: { id: string; name: string };
    user: { id: string; name: string; displayName: string };
    challenge: string; // base64url
    pubKeyCredParams: { type: "public-key"; alg: number }[];
    timeout?: number;
    excludeCredentials?: { id: string; type: "public-key" }[];
    authenticatorSelection?: PublicKeyCredentialCreationOptions["authenticatorSelection"];
    attestation?: AttestationConveyancePreference;
  };
}

export interface BeginAuthenticationOptions {
  publicKey: {
    challenge: string;
    rpId?: string;
    timeout?: number;
    allowCredentials?: { id: string; type: "public-key" }[];
    userVerification?: UserVerificationRequirement;
  };
}

export interface RegistrationResult {
  id: string;
  rawId: string;
  type: "public-key";
  response: { clientDataJSON: string; attestationObject: string };
}

export interface AuthenticationResult {
  id: string;
  rawId: string;
  type: "public-key";
  response: {
    clientDataJSON: string;
    authenticatorData: string;
    signature: string;
    userHandle: string | null;
  };
}

function b64uToBuf(b64u: string): ArrayBuffer {
  const b64 = b64u.replace(/-/g, "+").replace(/_/g, "/");
  const pad = b64.length % 4 ? "=".repeat(4 - (b64.length % 4)) : "";
  const bin = atob(b64 + pad);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function bufToB64u(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export async function startRegistration(opts: BeginRegistrationOptions): Promise<RegistrationResult> {
  const k = opts.publicKey;
  const publicKey: PublicKeyCredentialCreationOptions = {
    rp: k.rp,
    user: { ...k.user, id: new Uint8Array(b64uToBuf(k.user.id)) },
    challenge: b64uToBuf(k.challenge),
    pubKeyCredParams: k.pubKeyCredParams,
    timeout: k.timeout,
    excludeCredentials: k.excludeCredentials?.map((c) => ({ id: b64uToBuf(c.id), type: c.type })),
    authenticatorSelection: k.authenticatorSelection,
    attestation: k.attestation,
  };
  const cred = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new Error("registration cancelled");
  const att = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: bufToB64u(cred.rawId),
    type: "public-key",
    response: {
      clientDataJSON: bufToB64u(att.clientDataJSON),
      attestationObject: bufToB64u(att.attestationObject),
    },
  };
}

export async function startAuthentication(opts: BeginAuthenticationOptions): Promise<AuthenticationResult> {
  const k = opts.publicKey;
  const publicKey: PublicKeyCredentialRequestOptions = {
    challenge: b64uToBuf(k.challenge),
    rpId: k.rpId,
    timeout: k.timeout,
    allowCredentials: k.allowCredentials?.map((c) => ({ id: b64uToBuf(c.id), type: c.type })),
    userVerification: k.userVerification,
  };
  const cred = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new Error("authentication cancelled");
  const a = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: bufToB64u(cred.rawId),
    type: "public-key",
    response: {
      clientDataJSON: bufToB64u(a.clientDataJSON),
      authenticatorData: bufToB64u(a.authenticatorData),
      signature: bufToB64u(a.signature),
      userHandle: a.userHandle ? bufToB64u(a.userHandle) : null,
    },
  };
}
