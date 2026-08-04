import { useState, useEffect } from 'react'
import { login, OIDC_LOGIN_URL } from '../api.js'

// Friendly messages for the ?auth_error= reasons the OIDC callback may redirect with.
const AUTH_ERRORS = {
  provider_error:  'The identity provider reported an error. Please try again.',
  missing_state:   'Your sign-in session expired. Please try again.',
  invalid_state:   'Your sign-in session was invalid. Please try again.',
  state_mismatch:  'Your sign-in session could not be verified. Please try again.',
  missing_code:    'The identity provider did not return an authorization code.',
  missing_subject: 'The identity provider did not return a user identifier.',
  exchange_failed: 'Could not complete sign-in with the identity provider.',
  signup_disabled: 'Your account is not provisioned. Contact an administrator.',
  server_error:    'An unexpected error occurred during sign-in.',
}

export default function LoginView({ onLogin, authConfig }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const oidcEnabled = authConfig?.oidc?.enabled === true
  const providerName = authConfig?.oidc?.provider_name || 'SSO'
  // Default to showing the password form unless the backend explicitly disables it.
  const passwordEnabled = authConfig ? authConfig.password_login !== false : true

  // Surface an OIDC callback error passed back as ?auth_error=…, then clean the URL.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const reason = params.get('auth_error')
    if (reason) {
      setError(AUTH_ERRORS[reason] || 'Sign-in failed. Please try again.')
      params.delete('auth_error')
      const qs = params.toString()
      window.history.replaceState(null, '', window.location.pathname + (qs ? `?${qs}` : ''))
    }
  }, [])

  async function handleSubmit(e) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const user = await login(username, password)
      onLogin(user)
    } catch (e) {
      setError(e.message || 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-container">
      <div className="login-card">
        <h1 className="app-title login-title">Montly</h1>
        {error && <p className="form-error" role="alert">{error}</p>}

        {oidcEnabled && (
          <a className="btn-primary login-btn sso-btn" href={OIDC_LOGIN_URL}>
            Sign in with {providerName}
          </a>
        )}

        {oidcEnabled && passwordEnabled && (
          <div className="login-divider"><span>or</span></div>
        )}

        {passwordEnabled && (
          <form onSubmit={handleSubmit} className="login-form">
            <div className="form-group">
              <label htmlFor="login-username">Username</label>
              <input
                id="login-username"
                type="text"
                value={username}
                onChange={e => setUsername(e.target.value)}
                required
                autoComplete="username"
                autoCapitalize="none"
                autoCorrect="off"
              />
            </div>
            <div className="form-group">
              <label htmlFor="login-password">Password</label>
              <input
                id="login-password"
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                required
                autoComplete="current-password"
              />
            </div>
            <button type="submit" className="btn-primary login-btn" disabled={loading}>
              {loading ? 'Signing in…' : 'Sign in'}
            </button>
          </form>
        )}
      </div>
    </div>
  )
}
