/**
 * Format a monetary amount using the user's number format preference.
 * @param {number} amount
 * @param {string} currency  - currency symbol (e.g. '€', '$')
 * @param {'en'|'eu'} numberFormat - 'en' → 1,234.56 · 'eu' → 1.234,56
 * @param {number} decimals  - fraction digits (default 2)
 */
export function formatAmount(amount, currency, numberFormat = 'en', decimals = 2) {
  const locale = numberFormat === 'eu' ? 'de-DE' : 'en-US'
  const n = new Intl.NumberFormat(locale, {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(amount)
  return `${currency}${n}`
}
