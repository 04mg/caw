/*
 * Copyright (c) 2013 Antoine Martin <antoine@xpra.org>
 * Copyright (c) 2016 David Brushinski <dbrushinski@spikes.com>
 * Copyright (c) 2014 Joshua Higgins <josh@kxes.net>
 * Copyright (c) 2015 Spikes, Inc.
 * Portions based on websock.js by Joel Martin
 * Copyright (C) 2012 Joel Martin
 *
 * Licensed under MPL 2.0
 *
 * Vendored from https://github.com/Xpra-org/xpra-html5 (html5/js/Protocol.js)
 * and adapted for Caw: converted to an ES module, removed the web-worker
 * host/bootstrap (we run the protocol on the main thread — decode happens
 * via createImageBitmap), and dropped the AES cipher paths (Caw never
 * enables xpra encryption). The on-the-wire framing is unchanged.
 *
 * requires:
 *  lz4.js
 *  rencode.js
 */

import { rencode, rdecode } from './rencode.js'
import { lz4 } from './lz4.js'

const CONNECT_TIMEOUT = 15_000

/**
 * The main Xpra wire protocol. Runs on the main thread: receives raw
 * WebSocket arraybuffers, reassembles 8-byte-header framed packets,
 * decompresses (lz4 only — we never advertise brotli), decodes the
 * rencode payload and hands the packet to a callback. Outgoing packets
 * are rencode-encoded and framed with a 0x10 protocol flag.
 */
export class XpraProtocol {
  constructor() {
    this.verify_connected_timer = 0
    this.packet_handler = null
    this.websocket = null
    this.raw_packets = []
    this.rQ = [] // Receive queue
    this.sQ = [] // Send queue
    this.header = []
    // Queue processing via intervals (ms); 0 = synchronous.
    this.process_interval = 0
  }

  close_event_str(event) {
    const code_mappings = {
      1000: 'Normal Closure',
      1001: 'Going Away',
      1002: 'Protocol Error',
      1003: 'Unsupported Data',
      1004: '(For future)',
      1005: 'No Status Received',
      1006: 'Abnormal Closure',
      1007: 'Invalid frame payload data',
      1008: 'Policy Violation',
      1009: 'Message too big',
      1010: 'Missing Extension',
      1011: 'Internal Error',
      1012: 'Service Restart',
      1013: 'Try Again Later',
      1014: 'Bad Gateway',
      1015: 'TLS Handshake',
    }
    let message = ''
    if (event.code) {
      try {
        message +=
          typeof code_mappings[event.code] !== 'undefined'
            ? `'${code_mappings[event.code]}' (${event.code})`
            : `${event.code}`
        if (event.reason) {
          message += `: '${event.reason}'`
        }
      } catch {
        message = 'unknown reason'
      }
    } else {
      message = 'unknown reason (no websocket error code)'
    }
    return message
  }

  open(uri) {
    const me = this
    // (re-)init
    this.raw_packets = []
    this.rQ = []
    this.sQ = []
    this.header = []
    this.websocket = null

    function handle(packet) {
      me.packet_handler(packet)
    }
    this.verify_connected_timer = setTimeout(
      () => handle(['error', 'connection timed out', 0]),
      CONNECT_TIMEOUT,
    )
    // connect the socket
    try {
      this.websocket = new WebSocket(uri, 'binary')
    } catch (error) {
      handle(['error', `${error}`, 0])
      return
    }
    this.websocket.binaryType = 'arraybuffer'
    this.websocket.addEventListener('open', () => {
      if (me.verify_connected_timer) {
        clearTimeout(me.verify_connected_timer)
        me.verify_connected_timer = 0
      }
      handle(['open'])
    })
    this.websocket.addEventListener('close', (event) =>
      handle(['close', me.close_event_str(event)]),
    )
    this.websocket.onerror = (event) =>
      handle(['error', me.close_event_str(event), event.code || 0])
    this.websocket.onmessage = function (e) {
      // push arraybuffer values onto the end
      me.rQ.push(new Uint8Array(e.data))
      setTimeout(function () {
        me.process_receive_queue()
      }, this.process_interval)
    }
  }

  close() {
    if (this.websocket) {
      this.websocket.onopen = null
      this.websocket.onclose = null
      this.websocket.onerror = null
      this.websocket.onmessage = null
      this.websocket.close()
      this.websocket = null
    }
  }

  protocol_error(message) {
    this.error('protocol error:', message)
    //make sure we stop processing packets and events:
    if (this.websocket) {
      this.websocket.onopen = null
      this.websocket.onclose = null
      this.websocket.onerror = null
      this.websocket.onmessage = null
    }
    this.header = []
    this.rQ = []
    //and just tell the client to close (it may still try to re-connect):
    this.packet_handler(['close', message])
  }

  process_receive_queue() {
    while (this.websocket && this.rQ.length > 0 && this.do_process_receive_queue());
  }

  error() {
    console.error.apply(console, arguments)
  }

  do_process_receive_queue() {
    /*
     * process data from this.rQ until we have enough for one packet chunk
     * then calls this.process_packet_data
     */
    if (this.header.length < 8 && this.rQ.length > 0) {
      //add from receive queue data to header until we get the 8 bytes we need:
      while (this.header.length < 8 && this.rQ.length > 0) {
        const slice = this.rQ[0]
        const needed = 8 - this.header.length
        const n = Math.min(needed, slice.length)
        this.header.push(...slice.subarray(0, n))
        if (slice.length > needed) {
          //replace the slice with what is left over:
          this.rQ[0] = slice.subarray(n)
        } else {
          //this slice has been fully consumed already:
          this.rQ.shift()
        }
      }

      //verify the header format:
      if (this.header[0] !== 80) {
        let message = `invalid packet header format: ${this.header[0]}`
        if (this.header.length > 1) {
          let hex = ''
          for (let p of this.header) {
            const v = p.toString(16)
            hex += v.length < 2 ? `0${v}` : v
          }
          message += `: 0x${hex}`
        }
        this.protocol_error(message)
        return false
      }
    }

    if (this.header.length < 8) {
      //we need more data to continue
      return true
    }

    //ignore 0x8: this flag is unused client-side:
    let proto_flags = this.header[1] & ~0x8
    const encrypted = proto_flags & 0x2
    if (encrypted) {
      proto_flags = proto_flags & ~0x2
    }
    if (encrypted) {
      this.protocol_error('encrypted packets are not supported by this client')
      return false
    }
    if (proto_flags > 1 && proto_flags !== 0x10) {
      this.protocol_error(`we can't handle this protocol flag yet: ${proto_flags}`)
      return false
    }

    let packet_size = [4, 5, 6, 7].reduce(
      (accumulator, value) => accumulator * 0x1_00 + this.header[value],
      0,
    )

    // verify that we have enough data for the full payload:
    let rsize = this.rQ.reduce((accumulator, value) => accumulator + value.length, 0)
    if (rsize < packet_size) {
      return false
    }

    // done parsing the header, the next packet will need a new one:
    const header = this.header
    this.header = []

    let packet_data
    if (this.rQ[0].length === packet_size) {
      //exact match: the payload is in a buffer already:
      packet_data = this.rQ.shift()
    } else {
      //aggregate all the buffers into "packet_data" until we get exactly "packet_size" bytes:
      packet_data = new Uint8Array(packet_size)
      rsize = 0
      while (rsize < packet_size) {
        const slice = this.rQ[0]
        const needed = packet_size - rsize
        if (slice.length > needed) {
          //add part of this slice:
          packet_data.set(slice.subarray(0, needed), rsize)
          rsize += needed
          this.rQ[0] = slice.subarray(needed)
        } else {
          //add this slice in full:
          packet_data.set(slice, rsize)
          rsize += slice.length
          this.rQ.shift()
        }
      }
    }

    this.process_packet_data(header, packet_data)
    return true
  }

  process_packet_data(header, packet_data) {
    /*
     * the packet data has been decrypted (if needed),
     * decompress it (if needed),
     * then either store it if it is a chunk,
     * or decode the packet if we have received all the chunks (chunk no is 0)
     */
    const level = header[2]
    const index = header[3]

    //decompress it if needed:
    if (level !== 0) {
      let inflated
      if (level & 0x10) {
        inflated = lz4.decode(packet_data)
      } else if (level & 0x40) {
        this.protocol_error('brotli compression is not supported')
        return
      } else {
        this.protocol_error(`unsupported compressor specified: ${level}`)
        return
      }
      packet_data = inflated
    }

    //save it for later? (partial raw packet)
    if (index > 0) {
      if (index >= 20) {
        this.protocol_error(`invalid packet index: ${index}`)
        return
      }
      this.raw_packets[index] = packet_data
      if (this.raw_packets.length >= 4) {
        this.protocol_error(`too many raw packets: ${this.raw_packets.length}`)
      }
      return
    }

    //decode raw packet data into objects:
    let packet = null
    try {
      packet = rdecode(packet_data)
      for (const index in this.raw_packets) {
        packet[index] = this.raw_packets[index]
      }
      this.raw_packets = {}
    } catch (error) {
      //FIXME: maybe we should error out and disconnect here?
      this.error('error decoding packet', error)
      this.error(`packet=${packet_data}`)
      const proto_flags = header[1]
      this.error(`protocol flags=${proto_flags}`)
      this.error(` level=${level}`)
      this.error(` index=${index}`)
      this.raw_packets = []
      return
    }

    try {
      // call the packet handler
      this.packet_handler(packet)
    } catch (error) {
      //FIXME: maybe we should error out and disconnect here?
      this.error(`error processing packet ${packet[0]}: ${error}`)
      this.error(` packet data: ${packet_data}`)
    }
  }

  process_send_queue() {
    while (this.sQ.length > 0 && this.websocket) {
      const packet = this.sQ.shift()
      if (!packet) {
        return
      }
      let bdata = null
      try {
        bdata = rencode(packet)
      } catch (error) {
        this.error('Error: failed to encode packet:', packet)
        this.error(error)
        continue
      }
      const payload_size = bdata.length
      this.send_packet(bdata, payload_size, false)
    }
  }

  make_packet_header(proto_flags, level, payload_size) {
    const header = new Uint8Array(8)
    header[0] = 'P'.charCodeAt(0)
    header[1] = proto_flags
    header[2] = level
    header[3] = 0
    //size header:
    for (let index = 0; index < 4; index++) {
      header[7 - index] = (payload_size >> (8 * index)) & 0xff
    }
    return header
  }

  send_packet(bdata, payload_size, encrypted) {
    const level = 0
    let proto_flags = 0x10
    if (encrypted) {
      proto_flags |= 0x2
    }
    const header = this.make_packet_header(proto_flags, level, payload_size)
    const actual_size = bdata.byteLength
    const packet = new Uint8Array(8 + actual_size)
    packet.set(header, 0)
    packet.set(bdata, 8)
    // put into buffer before send
    if (this.websocket) {
      this.websocket.send(packet.buffer)
    }
  }

  send(packet) {
    this.sQ[this.sQ.length] = packet
    setTimeout(() => this.process_send_queue(), this.process_interval)
  }

  set_packet_handler(callback) {
    this.packet_handler = callback
  }
}