package ochazuke

import (
	"context"
	"testing"
	"time"
)

func TestRemoteTokenReplayKeepsNounCountQuerySequence(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	var asked []ListQuery
	r.Define("parcel").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(Store{
		Kind: KindCollection, Remote: true,
		ChangeToken: func(c Ctx) (string, error) {
			value, err := c.Await(StoreCall{Store: "parcel", Op: "token"}, func() Result {
				return Pending(func(context.Context) (any, error) { return "version-1", nil })
			}, time.Second)
			if err != nil {
				return "", err
			}
			return value.(string), nil
		},
		Query: func(c Ctx, q ListQuery) (ListPage, error) {
			key := "all"
			total := 12
			if len(q.Filters) > 0 {
				key, total = "archived", 3
			}
			value, err := c.Await(StoreCall{Store: "parcel", Op: "list", Key: key}, func() Result {
				asked = append(asked, q)
				return Pending(func(context.Context) (any, error) {
					return ListPage{Items: []Thing{{"id": "p1", "title": "Parcel"}}, Total: total}, nil
				})
			}, time.Second)
			if err != nil {
				return ListPage{}, err
			}
			return value.(ListPage), nil
		},
	})
	if err := r.EnsureNounsPage(); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage(NounsPageID); err != nil {
		t.Fatal(err)
	}
	settleAll(t, r)
	if count, archived, known, hasArchived := r.NounCount("parcel"); count != 12 || archived != 3 || !known || !hasArchived {
		t.Fatalf("cold remote count=%d archived=%d known=%v hasArchived=%v; jobs=%+v", count, archived, known, hasArchived, r.Jobs())
	}
	if row := nounRow(t, r, "parcel"); row.Cells[1] != "12" || row.Cells[2] != "3" {
		t.Fatalf("visible remote noun count: %+v", row)
	}
	if len(asked) != 2 || asked[0].Page != 1 || asked[0].PerPage != 1 || asked[1].PerPage != 1 || len(asked[1].Filters) != 1 {
		t.Fatalf("count must consume two bounded queries after the token: %+v", asked)
	}
}
