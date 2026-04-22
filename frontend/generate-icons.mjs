#!/usr/bin/env node
// Generates icon-192.png and icon-512.png for the PWA manifest.
// Run once: node frontend/generate-icons.mjs
// No external dependencies — pure Node.js + zlib.
import { deflateSync } from 'zlib'
import { writeFileSync } from 'fs'

// CRC32 lookup table
const CRC_TABLE = new Uint32Array(256).map((_, n) => {
  let c = n
  for (let k = 0; k < 8; k++) c = (c & 1) ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  return c
})
const crc32 = data => {
  let c = 0xffffffff
  for (const b of data) c = CRC_TABLE[(c ^ b) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}

function pngChunk(type, data) {
  const t = Buffer.from(type)
  const d = Buffer.from(data)
  const len = Buffer.allocUnsafe(4); len.writeUInt32BE(d.length)
  const crc = Buffer.allocUnsafe(4); crc.writeUInt32BE(crc32(Buffer.concat([t, d])))
  return Buffer.concat([len, t, d, crc])
}

// Icon design (at 192 × 192 base):
//   Background:  #2563eb (37, 99, 235)
//   Glyph "m":   white, centred
//
//   White block:  x ∈ [24,168)  y ∈ [44,148)   → 144 × 104 px
//   Cutout left:  x ∈ [48, 84)  y ∈ [72,148)   → left arch cavity
//   Cutout right: x ∈ [108,144) y ∈ [72,148)   → right arch cavity
//
//   Results in three 24 px stems joined by two 28 px arches at the top.

const BG = [37, 99, 235]
const FG = [255, 255, 255]

function pixelColor(x, y, size) {
  // Normalise to 192 design space
  const s  = size / 192
  const nx = x / s
  const ny = y / s

  const inBlock    = nx >= 24 && nx < 168 && ny >= 44 && ny < 148
  const inCutout1  = nx >= 48 && nx < 84  && ny >= 72 && ny < 148
  const inCutout2  = nx >= 108 && nx < 144 && ny >= 72 && ny < 148

  return (inBlock && !inCutout1 && !inCutout2) ? FG : BG
}

function generatePNG(size) {
  const stride = 1 + size * 3
  const raw    = Buffer.allocUnsafe(size * stride)

  for (let y = 0; y < size; y++) {
    raw[y * stride] = 0 // filter: None
    for (let x = 0; x < size; x++) {
      const [r, g, b] = pixelColor(x, y, size)
      const off = y * stride + 1 + x * 3
      raw[off] = r; raw[off + 1] = g; raw[off + 2] = b
    }
  }

  const ihdr = Buffer.allocUnsafe(13)
  ihdr.writeUInt32BE(size, 0)
  ihdr.writeUInt32BE(size, 4)
  ihdr[8] = 8;  // bit depth
  ihdr[9] = 2;  // colour type: RGB
  ihdr[10] = ihdr[11] = ihdr[12] = 0

  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), // PNG signature
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', deflateSync(raw, { level: 9 })),
    pngChunk('IEND', Buffer.alloc(0)),
  ])
}

writeFileSync('frontend/public/icon-192.png', generatePNG(192))
writeFileSync('frontend/public/icon-512.png', generatePNG(512))
console.log('✓ icon-192.png  (192 × 192)')
console.log('✓ icon-512.png  (512 × 512)')
