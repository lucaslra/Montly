// Cache Intl.NumberFormat instances — construction is expensive; reuse by locale+decimals.
const _fmtCache = new Map()
function _getFmt(locale, decimals) {
  const key = `${locale}:${decimals}`
  if (!_fmtCache.has(key)) {
    _fmtCache.set(key, new Intl.NumberFormat(locale, {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
    }))
  }
  return _fmtCache.get(key)
}

/**
 * Format a monetary amount using the user's number format preference.
 * @param {number} amount
 * @param {string} currency  - currency symbol (e.g. '€', '$')
 * @param {'en'|'eu'} numberFormat - 'en' → 1,234.56 · 'eu' → 1.234,56
 * @param {number} decimals  - fraction digits (default 2)
 */
export function formatAmount(amount, currency, numberFormat = 'en', decimals = 2) {
  const locale = numberFormat === 'eu' ? 'de-DE' : 'en-US'
  return `${currency}${_getFmt(locale, decimals).format(amount)}`
}
