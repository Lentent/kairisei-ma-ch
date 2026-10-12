package admin

import (
	"reflect"
	"testing"
)

func TestCustomCardArtworkCatalogReplacesPreviousTemplateAndStaleBucket(t *testing.T) {
	update := map[string]any{"name": "chr20_98000001", "directory": "05_image_assets/chr20/98/000", "bundle": "main_c/image/custom_card_98000001_new.dat"}
	other := map[string]any{"name": "chr20_98000003", "directory": "05_image_assets/chr20/98/000", "bundle": "main_c/image/custom_card_98000003_keep.dat"}
	unowned := map[string]any{"name": "chr20_98000001", "directory": "other_namespace", "bundle": "other.dat"}
	for _, rows := range [][]any{
		{map[string]any{"name": "chr20_98000001", "directory": "05_image_assets/chr20/98/000", "bundle": "main_c/image/custom_card_98000001_old.dat"}, other, unowned},
		{map[string]any{"name": "chr20_98000001", "directory": "05_image_assets/chr20/10/108", "bundle": "main_c/image/custom_card_98000001_old.dat"}, other, unowned},
		{map[string]any{"name": "CHR20_98000001", "directory": "05_IMAGE_ASSETS/CHR20/98/000", "bundle": "legacy.dat"}, other, unowned},
		{map[string]any{"name": "chr20_98000001", "directory": "05_image_assets/chr20/98/000", "bundle": "main_c/image/custom_card_98000001_old.dat"}, update, other, unowned},
	} {
		got := replaceCustomCardArtworkCatalogRow(rows, update, 98000001)
		if !reflect.DeepEqual(got, []any{update, other, unowned}) {
			t.Fatalf("replacement retained duplicate logical names or removed an unrelated card: %#v", got)
		}
	}
	if got := replaceCustomCardArtworkCatalogRow([]any{other, unowned}, update, 98000001); !reflect.DeepEqual(got, []any{other, unowned, update}) {
		t.Fatal("new card was not appended without changing existing rows")
	}
}
