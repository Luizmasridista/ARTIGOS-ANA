import { describe, it, expect } from "vitest"
import { isIpadUA } from "./useIpad"

describe("isIpadUA", () => {
  it("detecta iPad clássico", () => {
    const ua = "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
    expect(isIpadUA(ua, 5, true)).toBe(true)
  })
  it("detecta iPadOS 13+ como Macintosh com toque", () => {
    const ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"
    expect(isIpadUA(ua, 5, true)).toBe(true)
  })
  it("não confunde Mac sem toque", () => {
    const ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
    expect(isIpadUA(ua, 0, false)).toBe(false)
  })
  it("não confunde iPhone", () => {
    const ua = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15"
    expect(isIpadUA(ua, 5, true)).toBe(false)
  })
  it("iPad sem toque não detecta Mac", () => {
    const ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"
    expect(isIpadUA(ua, 0, false)).toBe(false)
  })
})
