// Minimal mock OpenID Connect provider for Montly E2E tests.
//
// Zero dependencies — uses only Node's built-in `http` and `crypto`. It
// implements just enough of the auth-code + PKCE flow for go-oidc to complete:
//   GET  /.well-known/openid-configuration  — discovery
//   GET  /jwks                              — signing key (RS256, exported as JWK)
//   GET  /authorize                         — auto-approves, 302s back with a code
//   POST /token                             — returns an RS256-signed id_token
//
// The single test identity is configurable via env vars. The nonce from the
// /authorize request is echoed into the id_token so the app's nonce check passes.

import http from 'node:http'
import crypto from 'node:crypto'

const PORT = Number(process.env.PORT || 9000)
const ISSUER = process.env.ISSUER || `http://mock-oidc:${PORT}`

const CLAIMS = {
  sub:                process.env.OIDC_SUB            || 'e2e-oidc-subject',
  preferred_username: process.env.OIDC_USERNAME       || 'e2e-sso',
  email:              process.env.OIDC_EMAIL          || 'e2e-sso@example.com',
  email_verified:    (process.env.OIDC_EMAIL_VERIFIED || 'true') === 'true',
  groups:            (process.env.OIDC_GROUPS         || 'montly-admins').split(',').filter(Boolean),
}

// Stable signing key generated at startup; exported as a JWK for the JWKS endpoint.
const { publicKey, privateKey } = crypto.generateKeyPairSync('rsa', { modulusLength: 2048 })
const KID = 'mock-key-1'
const jwk = { ...publicKey.export({ format: 'jwk' }), kid: KID, alg: 'RS256', use: 'sig' }

const b64url = (buf) => Buffer.from(buf).toString('base64url')

function signJWT(payload) {
  const header = b64url(JSON.stringify({ alg: 'RS256', typ: 'JWT', kid: KID }))
  const body = b64url(JSON.stringify(payload))
  const signingInput = `${header}.${body}`
  const sig = crypto.sign('RSA-SHA256', Buffer.from(signingInput), privateKey)
  return `${signingInput}.${b64url(sig)}`
}

// Outstanding authorization codes → { nonce, aud }.
const codes = new Map()

function sendJSON(res, obj, status = 200) {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(obj))
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, ISSUER)
  const p = url.pathname

  if (p === '/.well-known/openid-configuration') {
    return sendJSON(res, {
      issuer: ISSUER,
      authorization_endpoint: `${ISSUER}/authorize`,
      token_endpoint: `${ISSUER}/token`,
      jwks_uri: `${ISSUER}/jwks`,
      response_types_supported: ['code'],
      subject_types_supported: ['public'],
      id_token_signing_alg_values_supported: ['RS256'],
      scopes_supported: ['openid', 'profile', 'email'],
      token_endpoint_auth_methods_supported: ['client_secret_basic', 'client_secret_post'],
      claims_supported: ['sub', 'iss', 'aud', 'exp', 'iat', 'nonce', 'email', 'email_verified', 'preferred_username', 'groups'],
    })
  }

  if (p === '/jwks') {
    return sendJSON(res, { keys: [jwk] })
  }

  if (p === '/authorize') {
    const redirectUri = url.searchParams.get('redirect_uri')
    if (!redirectUri) {
      res.writeHead(400)
      return res.end('missing redirect_uri')
    }
    const code = crypto.randomBytes(16).toString('hex')
    codes.set(code, {
      nonce: url.searchParams.get('nonce') || '',
      aud: url.searchParams.get('client_id') || 'montly',
    })
    const back = new URL(redirectUri)
    const state = url.searchParams.get('state')
    if (state) back.searchParams.set('state', state)
    back.searchParams.set('code', code)
    res.writeHead(302, { Location: back.toString() })
    return res.end()
  }

  if (p === '/token' && req.method === 'POST') {
    let body = ''
    req.on('data', (c) => { body += c })
    req.on('end', () => {
      const params = new URLSearchParams(body)
      const entry = codes.get(params.get('code'))
      if (!entry) return sendJSON(res, { error: 'invalid_grant' }, 400)
      codes.delete(params.get('code'))

      const now = Math.floor(Date.now() / 1000)
      const idToken = signJWT({
        iss: ISSUER,
        aud: entry.aud,
        sub: CLAIMS.sub,
        iat: now,
        exp: now + 3600,
        nonce: entry.nonce,
        preferred_username: CLAIMS.preferred_username,
        email: CLAIMS.email,
        email_verified: CLAIMS.email_verified,
        groups: CLAIMS.groups,
      })
      return sendJSON(res, {
        access_token: 'mock-access-token',
        token_type: 'Bearer',
        expires_in: 3600,
        id_token: idToken,
      })
    })
    return
  }

  res.writeHead(404)
  res.end('not found')
})

server.listen(PORT, () => {
  console.log(`mock-oidc listening on :${PORT} (issuer=${ISSUER})`)
})
