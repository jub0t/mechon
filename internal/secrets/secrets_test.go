package secrets

import "testing"

func TestRoundTripAndBinding(t *testing.T) {
	box, err := Open(NewKey())
	if err != nil {
		t.Fatal(err)
	}
	ct := box.Seal([]byte("discord-token"), []byte("bot-a/DISCORD_TOKEN"))
	pt, err := box.Open(ct, []byte("bot-a/DISCORD_TOKEN"))
	if err != nil || string(pt) != "discord-token" {
		t.Fatalf("round trip: %q %v", pt, err)
	}
	if _, err := box.Open(ct, []byte("bot-b/DISCORD_TOKEN")); err == nil {
		t.Fatal("ciphertext opened under another bot's binding")
	}
	other, _ := Open(NewKey())
	if _, err := other.Open(ct, []byte("bot-a/DISCORD_TOKEN")); err == nil {
		t.Fatal("ciphertext opened with another key")
	}
}

func TestRejectsBadKeys(t *testing.T) {
	for _, k := range []string{"", "short", "bm90LTMyLWJ5dGVz"} {
		if _, err := Open(k); err == nil {
			t.Errorf("accepted key %q", k)
		}
	}
}
