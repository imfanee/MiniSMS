package carrier

import "testing"

func TestResolveTONNPI_StaticValues(t *testing.T) {
	got := ResolveTONNPI(SMPPConfig{
		SourceAddrTON: "5", SourceAddrNPI: "0", DestAddrTON: "1", DestAddrNPI: "1",
	}, "MyBrand", "+447700900123")
	if got.SourceAddrTON != 5 || got.SourceAddrNPI != 0 || got.DestAddrTON != 1 || got.DestAddrNPI != 1 {
		t.Fatalf("unexpected static result: %+v", got)
	}
}

func TestResolveTONNPI_DynamicAlphaSender(t *testing.T) {
	got := ResolveTONNPI(SMPPConfig{
		SourceAddrTON: "dynamic", SourceAddrNPI: "dynamic", DestAddrTON: "dynamic", DestAddrNPI: "dynamic",
	}, "MyBrand", "+447700900123")
	if got.SourceAddrTON != 5 || got.SourceAddrNPI != 0 || got.DestAddrTON != 1 || got.DestAddrNPI != 1 {
		t.Fatalf("unexpected dynamic alpha result: %+v", got)
	}
}

func TestResolveTONNPI_DynamicE164Sender(t *testing.T) {
	got := ResolveTONNPI(SMPPConfig{
		SourceAddrTON: "dynamic", SourceAddrNPI: "dynamic", DestAddrTON: "dynamic", DestAddrNPI: "dynamic",
	}, "+441234567890", "+447700900123")
	if got.SourceAddrTON != 1 || got.SourceAddrNPI != 1 || got.DestAddrTON != 1 || got.DestAddrNPI != 1 {
		t.Fatalf("unexpected dynamic e164 result: %+v", got)
	}
}

func TestResolveTONNPI_DynamicNumericSender(t *testing.T) {
	got := ResolveTONNPI(SMPPConfig{
		SourceAddrTON: "dynamic", SourceAddrNPI: "dynamic", DestAddrTON: "dynamic", DestAddrNPI: "dynamic",
	}, "447700111222", "+447700900123")
	if got.SourceAddrTON != 2 || got.SourceAddrNPI != 1 || got.DestAddrTON != 1 || got.DestAddrNPI != 1 {
		t.Fatalf("unexpected dynamic numeric result: %+v", got)
	}
}

func TestResolveTONNPI_MixedStaticDynamic(t *testing.T) {
	got := ResolveTONNPI(SMPPConfig{
		SourceAddrTON: "5", SourceAddrNPI: "dynamic", DestAddrTON: "dynamic", DestAddrNPI: "dynamic",
	}, "MyBrand", "+12125550100")
	if got.SourceAddrTON != 5 || got.DestAddrTON != 1 {
		t.Fatalf("unexpected mixed static/dynamic result: %+v", got)
	}
}
