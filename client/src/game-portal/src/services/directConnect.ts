// Direct-connect MP client — thin wrappers around the Go server's
// /api/direct-connect endpoints. Lives alongside NetworkClient because the
// proxy token it stores is consumed by NetworkClient's WS URL resolver.

const PROXY_TOKEN_STORAGE_KEY = 'webrts.directConnect.proxyToken'
const PROXY_HOST_STORAGE_KEY = 'webrts.directConnect.proxyHost'

export interface DirectConnectStatus {
  /** Server's current "Allow LAN/Internet connections" toggle state. */
  allow: boolean
}

export interface DirectConnectIps {
  ips: string[]
}

export interface JoinResult {
  token: string
  hostPort: string
}

export type DialErrorKind = 'timeout' | 'refused' | 'dns' | 'other'

export interface JoinError {
  kind: DialErrorKind
  message: string
}

/** GET current toggle state (off = only loopback allowed). */
export async function getToggleState(): Promise<DirectConnectStatus> {
  const r = await fetch('/api/direct-connect')
  if (!r.ok) throw new Error(`toggle GET: HTTP ${r.status}`)
  return r.json()
}

/** POST a new toggle state and return the server's confirmed state. */
export async function setToggleState(allow: boolean): Promise<DirectConnectStatus> {
  const r = await fetch('/api/direct-connect', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ allow }),
  })
  if (!r.ok) throw new Error(`toggle POST: HTTP ${r.status}`)
  return r.json()
}

/** GET the host's reachable non-loopback IPv4 addresses, sorted per §13.9
 *  (Tailscale CGNAT first, RFC1918 private next, everything else last). */
export async function getReachableIps(): Promise<DirectConnectIps> {
  const r = await fetch('/api/direct-connect/ips')
  if (!r.ok) throw new Error(`ips GET: HTTP ${r.status}`)
  return r.json()
}

/** Dial the host and request a proxy token. On success the token is stashed
 *  in sessionStorage; the next WS open will route through the joiner-as-proxy
 *  path to the remote host's hub. On failure the typed error includes a
 *  DialErrorKind so the UI can pick a sensible message. */
export async function joinHost(hostPort: string): Promise<JoinResult> {
  const r = await fetch('/api/direct-connect/join', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ hostPort }),
  })
  if (!r.ok) {
    let body: { error?: string; kind?: DialErrorKind } = {}
    try { body = await r.json() } catch { /* fall through */ }
    const err: JoinError = {
      kind: body.kind ?? 'other',
      message: body.error ?? `HTTP ${r.status}`,
    }
    throw err
  }
  const result: JoinResult = await r.json()
  try {
    sessionStorage.setItem(PROXY_TOKEN_STORAGE_KEY, result.token)
    sessionStorage.setItem(PROXY_HOST_STORAGE_KEY, result.hostPort)
  } catch {
    // sessionStorage can throw in some sandboxed contexts; the token is
    // returned anyway so the caller can pass it along by other means.
  }
  return result
}

/** Returns true when the SPA is currently in proxy mode (i.e., next WS
 *  open will route through a remote host). */
export function isProxyActive(): boolean {
  try {
    return !!sessionStorage.getItem(PROXY_TOKEN_STORAGE_KEY)
  } catch {
    return false
  }
}

/** Returns the remote host:port the SPA is proxying to, or null. */
export function activeProxyHost(): string | null {
  try {
    return sessionStorage.getItem(PROXY_HOST_STORAGE_KEY)
  } catch {
    return null
  }
}

/** Clear the proxy token so subsequent WS opens use the local hub again.
 *  Caller is responsible for closing the existing WS (a page reload is the
 *  simplest, used by DirectConnect.vue). */
export function clearProxy(): void {
  try {
    sessionStorage.removeItem(PROXY_TOKEN_STORAGE_KEY)
    sessionStorage.removeItem(PROXY_HOST_STORAGE_KEY)
  } catch {
    // ignore — the worst case is a stale token survives until the tab closes
  }
}

/** Convert a DialError kind into descriptive UI copy (§13.5). */
export function dialErrorMessage(err: JoinError): string {
  switch (err.kind) {
    case 'timeout':
      return 'Could not reach the host within 5 seconds. Check the address, your network, and that the host has "Allow LAN/Internet connections" enabled.'
    case 'refused':
      return 'The host refused the connection. Check that the port is correct and that the host has "Allow LAN/Internet connections" enabled.'
    case 'dns':
      return 'Could not resolve the host name. Check spelling or use a numeric IP address.'
    case 'other':
    default:
      return `Connection failed: ${err.message}`
  }
}
