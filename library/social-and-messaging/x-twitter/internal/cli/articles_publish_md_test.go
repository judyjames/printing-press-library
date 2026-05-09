package cli

import "testing"

func TestMarkdownBodyToDraftJSImageLine(t *testing.T) {
	contentState := MarkdownBodyToDraftJS("Before\n\n![body alt](./body.png)\n\nAfter")

	if len(contentState.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(contentState.Blocks))
	}
	if contentState.Blocks[1].Type != "atomic" {
		t.Fatalf("expected image line to produce an atomic block, got %q", contentState.Blocks[1].Type)
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
		t.Fatalf("expected one media entity, got %d", len(contentState.EntityMap))
	}
	entity := contentState.EntityMap[0]
	if entity.Value.Type != "MEDIA" {
		t.Fatalf("expected MEDIA entity, got %q", entity.Value.Type)
	}
	if entity.Value.Mutability != "Immutable" {
		t.Fatalf("expected Immutable entity, got %q", entity.Value.Mutability)
	}
	if entity.Value.Data["source_path"] != "./body.png" {
		t.Fatalf("expected source_path to be retained, got %#v", entity.Value.Data["source_path"])
	}
	if entity.Value.Data["alt_text"] != "body alt" {
		t.Fatalf("expected alt_text to be retained, got %#v", entity.Value.Data["alt_text"])
	}
}

func TestBindArticleMediaEntities(t *testing.T) {
	contentState := MarkdownBodyToDraftJS("![one](./one.png)\n\n![two](./two.jpg)")
	uploads := []string{}

	err := bindArticleMediaEntities(&contentState, func(path string) (string, error) {
		uploads = append(uploads, path)
		return "media-" + path, nil
	})
	if err != nil {
		t.Fatalf("bindArticleMediaEntities returned error: %v", err)
	}
	if len(uploads) != 2 {
		t.Fatalf("expected 2 uploads, got %d", len(uploads))
	}
	if uploads[0] != "./one.png" || uploads[1] != "./two.jpg" {
		t.Fatalf("unexpected upload paths: %#v", uploads)
	}

	first := contentState.EntityMap[0].Value
	if first.Data["source_path"] != nil {
		t.Fatalf("expected source_path to be removed after bind, got %#v", first.Data["source_path"])
	}
	firstItems, ok := first.Data["media_items"].([]map[string]any)
	if !ok || len(firstItems) != 1 {
		t.Fatalf("expected first media_items, got %#v", first.Data["media_items"])
	}
	if firstItems[0]["local_media_id"] != 2 {
		t.Fatalf("expected first local_media_id 2, got %#v", firstItems[0]["local_media_id"])
	}
	if firstItems[0]["media_category"] != "DraftTweetImage" {
		t.Fatalf("expected DraftTweetImage, got %#v", firstItems[0]["media_category"])
	}
	if firstItems[0]["media_id"] != "media-./one.png" {
		t.Fatalf("expected first media_id, got %#v", firstItems[0]["media_id"])
	}

	second := contentState.EntityMap[1].Value
	secondItems, ok := second.Data["media_items"].([]map[string]any)
	if !ok || len(secondItems) != 1 {
		t.Fatalf("expected second media_items, got %#v", second.Data["media_items"])
	}
	if secondItems[0]["local_media_id"] != 4 {
		t.Fatalf("expected second local_media_id 4, got %#v", secondItems[0]["local_media_id"])
	}
	if secondItems[0]["media_id"] != "media-./two.jpg" {
		t.Fatalf("expected second media_id, got %#v", secondItems[0]["media_id"])
	}
}

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
