package apicontract

import (
	"strings"
	"testing"
)

func TestLocalFileSaveContract(t *testing.T) {
	request, err := LocalMarkdown.Decode(strings.NewReader(`{"action":"bind","id":"doc","path":"C:/note.txt","revision":""}`))
	if err != nil || request.Revision == nil || *request.Revision != "" {
		t.Fatal(request, err)
	}
	omitted, err := LocalMarkdown.Decode(strings.NewReader(`{"action":"bind","id":"doc","path":"C:/note.txt"}`))
	if err != nil || omitted.Revision != nil {
		t.Fatal("missing revision lost its distinction from an empty-file revision", omitted, err)
	}
	for _, body := range []string{
		`{"action":"file","path":1}`, `{"action":"bind","revision":false}`, `{"action":"bind","revision":[]}`,
	} {
		if _, err = LocalMarkdown.Decode(strings.NewReader(body)); err == nil {
			t.Fatal("invalid local save request accepted", body)
		}
	}
}
