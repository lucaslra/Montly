import { describe, it, expect } from 'vitest'
import { formatAmount } from '../utils.js'

describe('formatAmount', () => {
  describe('en (en-US) number format', () => {
    it('formats a simple value with currency symbol prefix', () => {
      expect(formatAmount(12.5, '$')).toBe('$12.50')
    })

    it('uses comma as thousands separator and period as decimal', () => {
      expect(formatAmount(1234.56, '$')).toBe('$1,234.56')
    })

    it('pads to two decimal places by default', () => {
      expect(formatAmount(9, '£')).toBe('£9.00')
    })

    it('handles zero', () => {
      expect(formatAmount(0, '$')).toBe('$0.00')
    })

    it('respects a custom decimal count of 0', () => {
      expect(formatAmount(100, '$', 'en', 0)).toBe('$100')
    })

    it('respects a custom decimal count of 3', () => {
      expect(formatAmount(1.5, '$', 'en', 3)).toBe('$1.500')
    })

    it('works with arbitrary currency symbols', () => {
      expect(formatAmount(9.99, '¥')).toBe('¥9.99')
    })

    it('formats large amounts with multiple comma groups', () => {
      expect(formatAmount(1000000, '$')).toBe('$1,000,000.00')
    })
  })

  describe('eu (de-DE) number format', () => {
    it('uses period as thousands separator and comma as decimal', () => {
      expect(formatAmount(1234.56, '€', 'eu')).toBe('€1.234,56')
    })

    it('pads to two decimal places', () => {
      expect(formatAmount(9, '€', 'eu')).toBe('€9,00')
    })

    it('handles zero', () => {
      expect(formatAmount(0, '€', 'eu')).toBe('€0,00')
    })

    it('respects a custom decimal count of 0', () => {
      expect(formatAmount(100, '€', 'eu', 0)).toBe('€100')
    })
  })
})
