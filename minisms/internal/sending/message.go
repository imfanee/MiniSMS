// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

// IngressTransport identifies how a message entered the shared pipeline.
type IngressTransport string

const (
	IngressHTTP IngressTransport = "http"
	IngressSMPP IngressTransport = "smpp"
)

// EgressTransport identifies which carrier transport accepted the dispatch.
type EgressTransport string

const (
	EgressHTTP EgressTransport = "http"
	EgressSMPP EgressTransport = "smpp"
)

// EgressTransport values match sms_logs.egress_transport CHECK constraint.

// AcceptedMessage is the transport-agnostic input to the shared send pipeline.
type AcceptedMessage struct {
	To               string
	From             string
	Body             string
	ClientRef        string
	DLRRequested     bool
	DLRWebhookURL    *string
	IngressTransport IngressTransport
}
