package control

import (
	"encoding/json"
	"testing"
)

func TestResponseFinal(t *testing.T) {
	for _, interim := range []Response{{Status: StatusAccepted}, {Status: StatusWorking}} {
		if interim.Final() {
			t.Errorf("%+v is final", interim)
		}
	}
	for _, final := range []Response{{Status: StatusDone}, {Error: "failed"}, {}} {
		if !final.Final() {
			t.Errorf("%+v is not final", final)
		}
	}
}

func TestClientWaitsLongerThanTheHostReportsProgress(t *testing.T) {
	if RequestTimeout <= ProgressInterval*3 {
		t.Fatalf("RequestTimeout %s leaves no room for progress every %s", RequestTimeout, ProgressInterval)
	}
}

func TestResponseWireFormat(t *testing.T) {
	data, err := json.Marshal(Response{Status: StatusAccepted})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"status":"accepted"}` {
		t.Fatalf("encoded %s", data)
	}
}
