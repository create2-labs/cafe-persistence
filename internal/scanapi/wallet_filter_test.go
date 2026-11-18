package scanapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/scanroutes"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
)

// CA2.2
func TestWalletEntityMatchesChainID(t *testing.T) {
	cases := []struct {
		name    string
		chains  string
		unobs   string
		want    bool
		wantErr bool
	}{
		{"eoa", `[{"chain_id":10,"qualification":"eoa"}]`, "", true, false},
		{"contract", `[{"chain_id":10,"qualification":"contract"}]`, "", true, false},
		{"not_deployed", `[{"chain_id":10,"qualification":"not_deployed"}]`, "", true, false},
		{"blank qualification", `[{"chain_id":10,"qualification":"blank"}]`, "", true, false},
		{"other chain only", `[{"chain_id":11,"qualification":"eoa"}]`, "", false, false},
		{"only unobserved", `[{"chain_id":11,"qualification":"eoa"}]`, "[10]", false, false},
		{"empty column", "", "", false, false},
		{"empty array", "[]", "", false, false},
		{"not json", "{not json", "", false, true},
		{"null", "null", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := walletEntityMatchesChainID(&domain.ScanResultEntity{Chains: tc.chains, UnobservedChainIDs: tc.unobs}, 10)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got (%v, %v), want (%v, err=%v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func getWalletList(t *testing.T, base string, user uuid.UUID, query string) (int, map[string]any) {
	t.Helper()
	resp := doAuthRequest(t, http.MethodGet, base+scanroutes.V1Base+scanroutes.WalletScans+"?"+query, user, nil)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, out
}

func listIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, _ := body["items"].([]any)
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	return ids
}

func seedChainRows(t *testing.T, user uuid.UUID, rows []domain.ScanResultEntity) (*uuidServer, []uuid.UUID) {
	t.Helper()
	db := setupScanAPITestDB(t)
	base := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		r.ID, r.UserID, r.Address = uuid.New(), user, "0xabc"
		if r.Status == "" {
			r.Status = scan.StateSUCCESS
		}
		r.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		seedWalletRow(t, db, r)
		ids[i] = r.ID
	}
	return &uuidServer{newTestScanAPIServer(t, newMemoryPendingRepo(), db).URL}, ids
}

type uuidServer struct{ URL string }

// CA2.2
func TestScanAPI_ListWalletScans_ChainFilter(t *testing.T) {
	user := uuid.New()
	srv, ids := seedChainRows(t, user, []domain.ScanResultEntity{
		{Chains: `[{"chain_id":10,"qualification":"eoa"}]`},
		{Chains: `[{"chain_id":1,"qualification":"eoa"}]`, UnobservedChainIDs: "[10]"},
		{Chains: `[{"chain_id":10,"qualification":"not_deployed"}]`},
		{Chains: `[{"chain_id":10,"qualification":"contract"}]`},
	})
	// Newest first: ids[3], ids[2], ids[0]; ids[1] is linked through unobserved_chain_ids only.
	code, body := getWalletList(t, srv.URL, user, "address=0xabc&chain_id=10&limit=2")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got := listIDs(t, body); len(got) != 2 || got[0] != ids[3].String() || got[1] != ids[2].String() {
		t.Fatalf("page 1 = %v, want [%s %s]", got, ids[3], ids[2])
	}
	if body["total"] != float64(3) {
		t.Fatalf("total = %v, want 3", body["total"])
	}
	_, body = getWalletList(t, srv.URL, user, "address=0xabc&chain_id=10&limit=2&offset=2")
	if got := listIDs(t, body); len(got) != 1 || got[0] != ids[0].String() || body["total"] != float64(3) {
		t.Fatalf("page 2 = %v total=%v, want [%s] total 3", got, body["total"], ids[0])
	}
}

// CA2.2
func TestScanAPI_ListWalletScans_LatestChainFilter(t *testing.T) {
	user := uuid.New()
	srv, ids := seedChainRows(t, user, []domain.ScanResultEntity{
		{Chains: `[{"chain_id":10,"qualification":"eoa"}]`},
		{Chains: `[{"chain_id":10,"qualification":"eoa"}]`, Status: scan.StatePENDING},
		{Chains: `[{"chain_id":1,"qualification":"eoa"}]`, UnobservedChainIDs: "[10]"},
	})
	code, body := getWalletList(t, srv.URL, user, "address=0xabc&latest=true&chain_id=10")
	if got := listIDs(t, body); code != http.StatusOK || len(got) != 1 || got[0] != ids[0].String() {
		t.Fatalf("latest = %d %v, want [%s]", code, got, ids[0])
	}
}

// CA2.2
func TestScanAPI_ListWalletScans_ChainFilterUnreadableChainsIs503(t *testing.T) {
	user := uuid.New()
	srv, _ := seedChainRows(t, user, []domain.ScanResultEntity{{Chains: "not json"}})
	for _, q := range []string{"address=0xabc&chain_id=10", "address=0xabc&latest=true&chain_id=10"} {
		code, body := getWalletList(t, srv.URL, user, q)
		if code != http.StatusServiceUnavailable || body["error"] != "service_unavailable" {
			t.Fatalf("%s = %d %#v, want 503 service_unavailable", q, code, body)
		}
	}
}

// CA2.2
func TestScanAPI_ListWalletScans_ChainFilterBadRequest(t *testing.T) {
	user := uuid.New()
	srv, _ := seedChainRows(t, user, nil)
	for _, q := range []string{"chain_id=10", "address=0xabc&chain_id=0", "address=0xabc&chain_id=-1"} {
		if code, _ := getWalletList(t, srv.URL, user, q); code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", q, code)
		}
	}
}
