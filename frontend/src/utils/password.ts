export function secureRandomInt(max: number): number {
  if (max <= 0) return 0
  const arr = new Uint32Array(1)
  crypto.getRandomValues(arr)
  return arr[0] % max
}

export function generateAdPassword(length = 8): string {
  const uppers = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
  const lowers = 'abcdefghijklmnopqrstuvwxyz'
  const digits = '0123456789'
  const all = `${uppers}${lowers}${digits}`
  const chars = [
    uppers[secureRandomInt(uppers.length)],
    lowers[secureRandomInt(lowers.length)],
    digits[secureRandomInt(digits.length)],
  ]
  while (chars.length < length) {
    chars.push(all[secureRandomInt(all.length)])
  }
  for (let i = chars.length - 1; i > 0; i -= 1) {
    const j = secureRandomInt(i + 1)
    ;[chars[i], chars[j]] = [chars[j], chars[i]]
  }
  return chars.join('')
}
