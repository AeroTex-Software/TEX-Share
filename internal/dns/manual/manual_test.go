package manual

import (
	"context"
	"testing"

	"github.com/AeroTex-Software/TEX-Share/internal/dns/provider"
)

func TestManualNeverFakesCreation(t *testing.T) {
	p := New()
	ctx := context.Background()
	if _, err := p.Zones(ctx); err != provider.ErrUnsupported {
		t.Fatalf("Zones err = %v, want ErrUnsupported", err)
	}
	if _, _, err := p.FindRecord(ctx, "z", "n", "A"); err != provider.ErrUnsupported {
		t.Fatalf("FindRecord err = %v, want ErrUnsupported", err)
	}
	if _, err := p.CreateRecord(ctx, "z", provider.Record{Type: "A"}); err != provider.ErrUnsupported {
		t.Fatalf("CreateRecord err = %v, want ErrUnsupported", err)
	}
	if err := p.DeleteRecord(ctx, "z", "r"); err != provider.ErrUnsupported {
		t.Fatalf("DeleteRecord err = %v, want ErrUnsupported", err)
	}
}

func TestInstructionsDirect(t *testing.T) {
	out := Instructions("share.example.com", "direct", "A", "203.0.113.10", "", "")
	if len(out) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(out))
	}
	if out[0].Type != "A" || out[0].Name != "share" || out[0].Content != "203.0.113.10" {
		t.Fatalf("unexpected instruction: %+v", out[0])
	}
}

func TestInstructionsProxyCNAME(t *testing.T) {
	out := Instructions("share.example.com", "proxy", "CNAME", "203.0.113.10", "", "")
	if len(out) != 1 || out[0].Type != "CNAME" {
		t.Fatalf("expected CNAME instruction, got %+v", out)
	}
}

func TestInstructionsTunnel(t *testing.T) {
	out := Instructions("share.example.com", "tunnel", "CNAME", "", "", "abc.cfargotunnel.com")
	if len(out) != 1 || out[0].Content != "abc.cfargotunnel.com" {
		t.Fatalf("unexpected tunnel instruction: %+v", out)
	}
}
