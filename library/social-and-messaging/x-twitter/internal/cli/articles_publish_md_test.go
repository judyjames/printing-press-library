package cli

import "testing"

func TestMarkdownBodyToDraftJSCodeFence(t *testing.T) {
	contentState := MarkdownBodyToDraftJS("Before\n\n```bash\nx-twitter-pp-cli articles-publish-md draft.md --post\n```\n\nAfter")

	if len(contentState.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(contentState.Blocks))
	}
	if contentState.Blocks[1].Type != "atomic" {
		t.Fatalf("expected fenced code to produce an atomic block, got %q", contentState.Blocks[1].Type)
	}
	if contentState.Blocks[1].Text != " " {
		t.Fatalf("expected atomic block text to be a single space, got %q", contentState.Blocks[1].Text)
	}
	if len(contentState.Blocks[1].EntityRanges) != 1 {
		t.Fatalf("expected one entity range, got %d", len(contentState.Blocks[1].EntityRanges))
	}
	if contentState.Blocks[1].EntityRanges[0]["key"] != 0 {
		t.Fatalf("expected atomic block to reference entity key 0, got %#v", contentState.Blocks[1].EntityRanges[0]["key"])
	}
	if len(contentState.EntityMap) != 1 {
		t.Fatalf("expected one markdown entity, got %d", len(contentState.EntityMap))
	}
	entity := contentState.EntityMap[0]
	if entity.Key != "0" {
		t.Fatalf("expected entity key 0, got %q", entity.Key)
	}
	if entity.Value.Type != "MARKDOWN" {
		t.Fatalf("expected MARKDOWN entity, got %q", entity.Value.Type)
	}
	if entity.Value.Mutability != "Mutable" {
		t.Fatalf("expected Mutable entity, got %q", entity.Value.Mutability)
	}
	wantMarkdown := "```bash\nx-twitter-pp-cli articles-publish-md draft.md --post\n```"
	if entity.Value.Data["markdown"] != wantMarkdown {
		t.Fatalf("unexpected markdown entity data:\nwant: %q\n got: %q", wantMarkdown, entity.Value.Data["markdown"])
	}
}
