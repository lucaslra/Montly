import { useState } from 'react'
import { setupAdmin } from '../api.js'

export default function SetupView({ onComplete }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm,  setConfirm]  = useState('')
  const [error,    setError]    = useState(null)
  const [loading,  setLoading]  = useState(false)
  const [showPw,   setShowPw]   = useState(false)
  const pwType = showPw ? 'text' : 'password'

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)
    if (password !== confirm) { setError('Passwords do not match'); return }
    if (password.length < 8)  { setError('Password must be at least 8 characters'); return }
    setLoading(true)
    try {
      const user = await setupAdmin(username.trim(), password)
      onComplete(user)
    } catch (err) {
      setError(err.message ?? 'Setup failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-container">
      <div className="login-card">
        <h1 className="app-title login-title">Montly</h1>
        <div>
          <p style={{ fontSize: '0.875rem', fontWeight: 600 }}>Welcome! Create your admin account to get started.</p>
        </div>
        {error && <p className="form-error" role="alert">{error}</p>}
        <form onSubmit={handleSubmit} className="login-form">
          <div className="form-group">
            <label htmlFor="setup-username">Username</label>
            <input
              id="setup-username"
              type="text"
              value={username}
              onChange={e => setUsername(e.target.value)}
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              autoFocus
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="setup-password">Password</label>
            <input
              id="setup-password"
              type={pwType}
              value={password}
              onChange={e => setPassword(e.target.value)}
              autoComplete="new-password"
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="setup-confirm">Confirm password</label>
            <input
              id="setup-confirm"
              type={pwType}
              value={confirm}
              onChange={e => setConfirm(e.target.value)}
              autoComplete="new-password"
              required
            />
          </div>
          <div className="form-group form-group-inline">
            <input id="setup-show" type="checkbox" checked={showPw} onChange={e => setShowPw(e.target.checked)} />
            <label htmlFor="setup-show">Show passwords</label>
          </div>
          <button type="submit" className="btn-primary login-btn" disabled={loading}>
            {loading ? 'Creating account…' : 'Create account'}
          </button>
        </form>
      </div>
    </div>
  )
}
