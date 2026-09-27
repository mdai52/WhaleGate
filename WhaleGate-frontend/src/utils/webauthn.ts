/**
 * WebAuthn / 通行密钥浏览器端工具。
 * 后端（go-webauthn）使用 base64url 编码各二进制字段，浏览器需要 ArrayBuffer，
 * 因此两端都要做编码转换。
 */

/** base64url → ArrayBuffer */
export function b64urlToBuffer(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/')
  const pad = padded.length % 4 === 0 ? '' : '='.repeat(4 - (padded.length % 4))
  const binary = atob(padded + pad)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer
}

/** ArrayBuffer → base64url */
export function bufferToB64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** 检查浏览器是否支持通行密钥 */
export function passkeySupported(): boolean {
  return typeof window !== 'undefined' && !!window.PublicKeyCredential
}

type UnknownRecord = Record<string, unknown>

/**
 * 把后端的 PublicKeyCredentialCreationOptions 转成浏览器可直接使用的对象。
 */
export function toCreationOptions(options: UnknownRecord): PublicKeyCredentialCreationOptions {
  const user = (options.user ?? {}) as UnknownRecord
  const exclude = (options.excludeCredentials ?? []) as UnknownRecord[]
  return {
    ...options,
    challenge: b64urlToBuffer(String(options.challenge)),
    user: {
      ...user,
      id: b64urlToBuffer(String(user.id)),
    },
    excludeCredentials: exclude.map((cred) => ({
      ...cred,
      id: b64urlToBuffer(String(cred.id)),
    })),
  } as unknown as PublicKeyCredentialCreationOptions
}

/** 把后端的 assertion options 转成浏览器对象 */
export function toRequestOptions(options: UnknownRecord): PublicKeyCredentialRequestOptions {
  const allow = (options.allowCredentials ?? []) as UnknownRecord[]
  return {
    ...options,
    challenge: b64urlToBuffer(String(options.challenge)),
    allowCredentials: allow.map((cred) => ({
      ...cred,
      id: b64urlToBuffer(String(cred.id)),
    })),
  } as unknown as PublicKeyCredentialRequestOptions
}

/** 把浏览器返回的凭据序列化为后端可解析的 JSON */
export function serializeAttestation(credential: PublicKeyCredential): UnknownRecord {
  const response = credential.response as AuthenticatorAttestationResponse
  return {
    id: credential.id,
    rawId: bufferToB64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToB64url(response.clientDataJSON),
      attestationObject: bufferToB64url(response.attestationObject),
    },
  }
}

/** 把浏览器返回的断言序列化为后端可解析的 JSON */
export function serializeAssertion(credential: PublicKeyCredential): UnknownRecord {
  const response = credential.response as AuthenticatorAssertionResponse
  return {
    id: credential.id,
    rawId: bufferToB64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToB64url(response.clientDataJSON),
      authenticatorData: bufferToB64url(response.authenticatorData),
      signature: bufferToB64url(response.signature),
      userHandle: response.userHandle ? bufferToB64url(response.userHandle) : null,
    },
  }
}
