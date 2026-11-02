package encrypt

import "testing"

func TestPKCS7UnpaddingRejectsMalformedPadding(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{name: "zero padding", plaintext: []byte("abcdefghijklmnop")},
		{name: "padding exceeds block size", plaintext: append([]byte("abcdefghijklmno"), 17)},
		{name: "padding exceeds buffer", plaintext: append([]byte("abcdefghijklmno"), 32)},
	}

	crypt := &WXBizMsgCrypt{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := crypt.pKCS7Unpadding(tt.plaintext, 16); err == nil {
				t.Fatal("expected malformed padding to be rejected")
			}
		})
	}
}

func TestPKCS7UnpaddingAcceptsValidPadding(t *testing.T) {
	crypt := &WXBizMsgCrypt{}
	got, err := crypt.pKCS7Unpadding([]byte("hello\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b"), 16)
	if err != nil {
		t.Fatalf("pKCS7Unpadding returned error: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("pKCS7Unpadding returned %q, want %q", got, "hello")
	}
}
