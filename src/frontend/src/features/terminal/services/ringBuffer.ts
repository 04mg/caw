const DEFAULT_CAPACITY = 10000

export class RingBuffer<T> {
  private buf: (T | undefined)[]
  private head = 0
  private count = 0
  private readonly capacity: number

  constructor(capacity = DEFAULT_CAPACITY) {
    this.capacity = capacity
    this.buf = new Array<T | undefined>(capacity)
  }

  push(item: T): void {
    this.buf[this.head] = item
    this.head = (this.head + 1) % this.capacity
    if (this.count < this.capacity) this.count++
  }

  get length(): number {
    return this.count
  }

  join(separator = ''): string {
    let result = ''
    const start = this.count === this.capacity ? this.head : 0
    for (let i = 0; i < this.count; i++) {
      const idx = (start + i) % this.capacity
      const item = this.buf[idx]
      if (item !== undefined) {
        result += i > 0 ? separator + item : item
      }
    }
    return result
  }

  clear(): void {
    this.buf = new Array<T | undefined>(this.capacity)
    this.head = 0
    this.count = 0
  }
}