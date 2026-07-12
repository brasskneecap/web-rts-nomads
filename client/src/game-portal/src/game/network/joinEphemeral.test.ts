import { describe, expect, it, vi, afterEach } from 'vitest'
import { NetworkClient } from './NetworkClient'
import { GameState } from '../core/GameState'

// Minimal fake WebSocket that captures every sent frame and fires onopen on
// the next microtask so NetworkClient's async join-message construction
// (which awaits getHashesForMap) runs to completion. Mirrors the real
// WebSocket's readyState contract just enough for NetworkClient.send() to
// treat the socket as open.
class FakeWebSocket {
  static OPEN = 1
  static CONNECTING = 0
  readyState = FakeWebSocket.CONNECTING
  sent: string[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((err: unknown) => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  url: string

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
    queueMicrotask(() => {
      this.readyState = FakeWebSocket.OPEN
      this.onopen?.()
    })
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = 3
  }

  static instances: FakeWebSocket[] = []
}

describe('NetworkClient join_match ephemeral option', () => {
  const originalWebSocket = globalThis.WebSocket

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket
    FakeWebSocket.instances = []
    vi.restoreAllMocks()
  })

  function mkClient(): NetworkClient {
    // @ts-expect-error — test double stands in for the DOM WebSocket class.
    globalThis.WebSocket = FakeWebSocket
    const state = new GameState()
    return new NetworkClient(state)
  }

  it('omits ephemeral from join_match by default', async () => {
    const client = mkClient()
    await client.connect({ resume: false })

    const ws = FakeWebSocket.instances[0]
    expect(ws.sent).toHaveLength(1)
    const joinMessage = JSON.parse(ws.sent[0])
    expect(joinMessage.type).toBe('join_match')
    expect(joinMessage.ephemeral).toBeUndefined()
  })

  it('sets ephemeral:true on join_match after setEphemeral(true)', async () => {
    const client = mkClient()
    client.setEphemeral(true)
    await client.connect({ resume: false })

    const ws = FakeWebSocket.instances[0]
    expect(ws.sent).toHaveLength(1)
    const joinMessage = JSON.parse(ws.sent[0])
    expect(joinMessage.type).toBe('join_match')
    expect(joinMessage.ephemeral).toBe(true)
  })
})
