export class XpraProtocol {
  packet_handler: ((packet: unknown[]) => void) | null
  process_interval: number
  constructor()
  open(uri: string): void
  close(): void
  send(packet: unknown[]): void
  set_packet_handler(callback: (packet: unknown[]) => void): void
  protocol_error(message: string): void
  process_receive_queue(): void
}