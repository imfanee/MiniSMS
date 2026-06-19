# Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
# Minimal dependency-free SMPP v3.4 transceiver client for testing MiniSMS ingress.
# Binds, submit_sm to a destination requesting a DLR, then prints the deliver_sm receipt.
import os, socket, struct, sys, time

# Configure via env or argv; no real credentials or numbers are stored here.
#   SMPP_HOST, SMPP_PORT, SMPP_SYSTEM_ID, SMPP_PASSWORD, SMPP_SRC
#   argv[1] = destination MSISDN (E.164 without '+')
HOST = os.getenv("SMPP_HOST", "127.0.0.1")
PORT = int(os.getenv("SMPP_PORT", "2775"))
SYS_ID = os.getenv("SMPP_SYSTEM_ID", "CHANGEME").encode()
PWD = os.getenv("SMPP_PASSWORD", "CHANGEME").encode()
SRC = os.getenv("SMPP_SRC", "DEMO").encode()
DST = (sys.argv[1] if len(sys.argv) > 1 else "10000000000").encode()
TEXT = b"MiniSMS ingress SMPP test"

BIND_TRX, SUBMIT_SM, DELIVER_SM = 0x00000009, 0x00000004, 0x00000005
ENQUIRE_LINK, UNBIND = 0x00000015, 0x00000006

def pdu(cmd_id, seq, body=b"", status=0):
    return struct.pack(">IIII", 16 + len(body), cmd_id, status, seq) + body

def cstr(b):
    return b + b"\x00"

def read_pdu(sock):
    hdr = b""
    while len(hdr) < 16:
        chunk = sock.recv(16 - len(hdr))
        if not chunk:
            return None
        hdr += chunk
    ln, cid, status, seq = struct.unpack(">IIII", hdr)
    body = b""
    while len(body) < ln - 16:
        chunk = sock.recv(ln - 16 - len(body))
        if not chunk:
            break
        body += chunk
    return cid, status, seq, body

def main():
    s = socket.create_connection((HOST, PORT), timeout=15)
    seq = 1
    # bind_transceiver
    body = cstr(SYS_ID) + cstr(PWD) + cstr(b"") + bytes([0x34, 0, 0]) + b"\x00"
    s.sendall(pdu(BIND_TRX, seq, body))
    cid, status, _, _ = read_pdu(s)
    print(f"bind_transceiver_resp: command_status=0x{status:02x} ({'OK' if status==0 else 'FAIL'})")
    if status != 0:
        sys.exit(2)

    # submit_sm to DST, request final delivery receipt (registered_delivery=1)
    seq += 1
    sm = TEXT
    body = (cstr(b"") +                       # service_type
            bytes([5, 0]) + cstr(SRC) +       # source TON/NPI + addr
            bytes([1, 1]) + cstr(DST) +       # dest TON/NPI + addr
            bytes([0, 0, 0]) +                # esm_class, protocol_id, priority
            cstr(b"") + cstr(b"") +           # schedule, validity
            bytes([1, 0, 0, 0]) +             # registered_delivery=1, replace, data_coding, sm_default
            bytes([len(sm)]) + sm)
    s.sendall(pdu(SUBMIT_SM, seq, body))
    cid, status, _, rbody = read_pdu(s)
    msg_id = rbody.split(b"\x00")[0].decode("latin-1") if rbody else ""
    print(f"submit_sm_resp: command_status=0x{status:02x} message_id={msg_id!r}")
    if status != 0:
        sys.exit(3)

    # Wait for deliver_sm (the DLR), answering enquire_link meanwhile.
    print("waiting for deliver_sm DLR (up to 90s)...")
    s.settimeout(90)
    deadline = time.time() + 90
    got_dlr = False
    while time.time() < deadline:
        try:
            res = read_pdu(s)
        except socket.timeout:
            break
        if res is None:
            print("connection closed by server")
            break
        cid, status, rseq, body = res
        if cid == DELIVER_SM:
            text = body.decode("latin-1", "replace")
            i = text.find("id:")
            receipt = text[i:] if i >= 0 else text
            print(f"deliver_sm DLR RECEIVED: {receipt.strip()}")
            s.sendall(pdu(0x80000005, rseq, b"\x00"))  # deliver_sm_resp
            got_dlr = True
            break
        elif cid == ENQUIRE_LINK:
            s.sendall(pdu(0x80000015, rseq))           # enquire_link_resp
        # ignore others

    seq += 1
    s.sendall(pdu(UNBIND, seq))
    try:
        read_pdu(s)
    except Exception:
        pass
    s.close()
    print("RESULT:", "DLR OK" if got_dlr else "NO DLR")
    sys.exit(0 if got_dlr else 4)

if __name__ == "__main__":
    main()
