package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateModContentCategoryTreeIdentifiesOverflowingCategory(t *testing.T) {
	categories := []modContentLayoutCategoryEdit{
		{PublicID: "target001", ParentPublicID: "root00001"},
		{PublicID: "target002", ParentPublicID: "target001"},
		{PublicID: "moving001", ParentPublicID: "target002"},
		{PublicID: "moving002", ParentPublicID: "moving001"},
		{PublicID: "moving003", ParentPublicID: "moving002"},
	}
	err := validateModContentCategoryTree("root00001", categories)
	validation, ok := err.(*modContentCategoryTreeError)
	if !ok {
		t.Fatalf("expected typed category tree error, got %T: %v", err, err)
	}
	if validation.CategoryID != "moving003" || validation.ParentID != "moving002" || validation.Reason != "maximum_depth_exceeded" {
		t.Fatalf("unexpected validation details: %#v", validation)
	}
}

func TestValidateModContentCategoryTreeAcceptsFourCategoryLevels(t *testing.T) {
	t.Parallel()
	categories := []modContentLayoutCategoryEdit{
		{PublicID: "category1", ParentPublicID: "root00001"},
		{PublicID: "category2", ParentPublicID: "category1"},
		{PublicID: "category3", ParentPublicID: "category2"},
		{PublicID: "category4", ParentPublicID: "category3"},
	}
	if err := validateModContentCategoryTree("root00001", categories); err != nil {
		t.Fatalf("valid four-level category tree rejected: %v", err)
	}
}

func TestWriteModContentLayoutPreparationErrorReturnsStableCategoryDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeModContentLayoutPreparationError(recorder, &modContentCategoryTreeError{
		CategoryID: "moving003",
		ParentID:   "moving002",
		Reason:     "maximum_depth_exceeded",
	})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code    string `json:"code"`
		Details struct {
			Field        string `json:"field"`
			CategoryID   string `json:"categoryId"`
			ParentID     string `json:"parentId"`
			Reason       string `json:"reason"`
			MaximumDepth int    `json:"maximumDepth"`
		} `json:"details"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "content_layout_category_tree_invalid" ||
		response.Details.Field != "categories.parentPublicId" ||
		response.Details.CategoryID != "moving003" || response.Details.ParentID != "moving002" ||
		response.Details.Reason != "maximum_depth_exceeded" || response.Details.MaximumDepth != 4 {
		t.Fatalf("unexpected response: %#v", response)
	}
}
