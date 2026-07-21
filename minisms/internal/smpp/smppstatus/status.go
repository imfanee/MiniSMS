// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.

// Package smppstatus decodes SMPP command_status values into the ESME_* mnemonic
// and a plain-English description with likely causes, so operators can read a
// failure and share an unambiguous reason with the upstream carrier or a client.
// It is shared by the egress (carrier) and ingress (client) SMPP paths.
package smppstatus

import "fmt"

type info struct {
	name string
	desc string
}

// Curated for the codes that actually matter when troubleshooting a bind or a
// submit. Descriptions call out the frequent real-world causes and, importantly,
// distinguish an SMPP-level rejection from a network/firewall problem (a bind can
// only be rejected after the TCP connection succeeded and the SMSC replied).
var table = map[int]info{
	0x00000000: {"ESME_ROK", "No error (accepted)."},
	0x00000001: {"ESME_RINVMSGLEN", "Invalid message length."},
	0x00000002: {"ESME_RINVCMDLEN", "Invalid command length."},
	0x00000003: {"ESME_RINVCMDID", "Invalid or unsupported command id."},
	0x00000004: {"ESME_RINVBNDSTS", "Command sent in the wrong bind state (e.g. submit on a receiver bind)."},
	0x00000005: {"ESME_RALYBND", "Already bound: the SMSC still holds a session for this system_id, or the concurrent-bind limit is reached. Often seen when stale sessions have not been reaped after a drop."},
	0x0000000A: {"ESME_RINVSRCADR", "Invalid source address (sender). Check sender TON/NPI and format."},
	0x0000000B: {"ESME_RINVDSTADR", "Invalid destination address (recipient). Check number format and dest TON/NPI."},
	0x0000000C: {"ESME_RINVMSGID", "Invalid message id."},
	0x0000000D: {"ESME_RBINDFAIL", "Bind rejected by the SMSC. This is an SMPP-level rejection, NOT a network/firewall block: the TCP connection succeeded and the SMSC replied. Common causes: the account's concurrent-bind limit is reached (or stale sessions still counted), the source IP is not allow-listed at the SMSC application layer, or the account is disabled."},
	0x0000000E: {"ESME_RINVPASWD", "Invalid password for this system_id."},
	0x0000000F: {"ESME_RINVSYSID", "Unknown/invalid system_id (the SMSC does not recognise this account)."},
	0x00000013: {"ESME_RREPLACEFAIL", "replace_sm failed."},
	0x00000014: {"ESME_RMSGQFUL", "SMSC message queue is full; retry later."},
	0x00000015: {"ESME_RINVSERTYP", "Invalid service_type."},
	0x00000045: {"ESME_RSUBMITFAIL", "submit_sm failed at the SMSC (generic). Check downstream capacity, routing, or account state."},
	0x00000048: {"ESME_RINVSRCTON", "Invalid source address TON."},
	0x00000049: {"ESME_RINVSRCNPI", "Invalid source address NPI."},
	0x00000050: {"ESME_RINVDSTTON", "Invalid destination address TON."},
	0x00000051: {"ESME_RINVDSTNPI", "Invalid destination address NPI."},
	0x00000058: {"ESME_RTHROTTLED", "Throttled by the SMSC: submit/bind rate exceeds the allowed limit. Slow down or reduce parallel binds."},
	0x00000062: {"ESME_RINVEXPIRY", "Invalid validity/expiry period."},
	0x000000FE: {"ESME_RDELIVERYFAILURE", "Delivery failure (in a delivery receipt)."},
	0x000000FF: {"ESME_RUNKNOWNERR", "Unknown SMSC error."},
}

// Name returns the ESME_* mnemonic for a command_status (or a hex fallback).
func Name(code int) string {
	if i, ok := table[code]; ok {
		return i.name
	}
	return fmt.Sprintf("0x%08X", uint32(code))
}

// Describe returns the mnemonic and a plain-English description with likely
// causes. Unknown codes still return a usable hex label.
func Describe(code int) (name, desc string) {
	if i, ok := table[code]; ok {
		return i.name, i.desc
	}
	return fmt.Sprintf("0x%08X", uint32(code)), "Unrecognised SMPP command_status; consult the SMPP v3.4 spec or the carrier."
}

// Format renders a single compact, human-readable string for a log line, e.g.
// "0x0000000D ESME_RBINDFAIL - Bind rejected by the SMSC...".
func Format(code int) string {
	name, desc := Describe(code)
	return fmt.Sprintf("0x%08X %s - %s", uint32(code), name, desc)
}
