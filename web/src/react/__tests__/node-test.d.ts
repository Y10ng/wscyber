declare module 'node:fs' {
  export function readFileSync(path: string, encoding: 'utf8'): string
}

declare module 'node:test' {
  export function describe(name: string, fn: () => void | Promise<void>): void
  export function it(name: string, fn: () => void | Promise<void>): void
}

declare module 'node:assert/strict' {
  export function equal<T>(actual: T, expected: T, message?: string): void
  export function notEqual<T>(actual: T, expected: T, message?: string): void
  export function deepEqual<T>(actual: T, expected: T, message?: string): void
  export function ok(value: unknown, message?: string): asserts value
  export function match(value: string, regExp: RegExp, message?: string): void
  export function doesNotMatch(value: string, regExp: RegExp, message?: string): void
  export function throws(block: () => unknown, error?: RegExp | ((err: unknown) => boolean)): void
  export function rejects(
    block: Promise<unknown> | (() => Promise<unknown>),
    error?: RegExp | ((err: unknown) => boolean),
  ): Promise<void>
  const assert: {
    equal: typeof equal
    notEqual: typeof notEqual
    deepEqual: typeof deepEqual
    ok: typeof ok
    match: typeof match
    doesNotMatch: typeof doesNotMatch
    throws: typeof throws
    rejects: typeof rejects
  }
  export default assert
}
