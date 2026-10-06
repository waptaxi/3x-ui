package sub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// TestGetSubs_OrdersBySubSortIndexThenId verifies that subscription output
// lists inbound links ordered by sub_sort_index ASC, breaking ties by id ASC.
// The same query feeds the raw body, the HTML sub page, and the JSON/Clash
// formats, so asserting on GetSubs covers all of them.
func TestGetSubs_OrdersBySubSortIndexThenId(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))

	const subId = "sub-sort"
	db := database.GetDB()

	seed := []struct {
		tag          string
		port         int
		subSortIndex int
		email        string
		uuid         string
	}{
		// Created in this order on purpose: without the ORDER BY the links
		// would come out s3, s1, s2a, s2b (creation order).
		{"sort-3", 42101, 3, "s3@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf001"},
		{"sort-1", 42102, 1, "s1@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf002"},
		{"sort-2a", 42103, 2, "s2a@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf003"},
		{"sort-2b", 42104, 2, "s2b@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf004"},
	}
	for _, s := range seed {
		settings := fmt.Sprintf(`{"clients": [{"id": %q, "email": %q, "subId": %q, "enable": true}]}`, s.uuid, s.email, subId)
		ib := &model.Inbound{
			UserId:         1,
			Tag:            s.tag,
			Enable:         true,
			Port:           s.port,
			Protocol:       model.VLESS,
			Settings:       settings,
			StreamSettings: `{"network": "tcp", "security": "none"}`,
			SubSortIndex:   s.subSortIndex,
		}
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", s.tag, err)
		}
		client := &model.ClientRecord{Email: s.email, SubID: subId, UUID: s.uuid, Enable: true}
		if err := db.Create(client).Error; err != nil {
			t.Fatalf("seed client %s: %v", s.email, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
			t.Fatalf("seed client_inbound %s: %v", s.email, err)
		}
	}

	s := NewSubService("")
	links, emails, _, _, err := s.GetSubs(subId, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != len(seed) {
		t.Fatalf("links = %d, want %d", len(links), len(seed))
	}
	want := []string{"s1@example.com", "s2a@example.com", "s2b@example.com", "s3@example.com"}
	for i, email := range want {
		if emails[i] != email {
			t.Fatalf("emails order = %v, want %v (sub_sort_index ASC, id ASC)", emails, want)
		}
	}
}

// TestGetSubs_PositionsInboundsBySubSortIndexAmongExternals pins what
// subSortIndex means in the emitted subscription: the 1-based slot the
// inbound's links occupy in the final list, with external links — which carry
// no index — filling the slots inbounds leave free in their own order. It
// fails while externals are appended after every inbound, the previous
// behaviour that made it impossible to place a higher-indexed inbound after an
// external link.
func TestGetSubs_PositionsInboundsBySubSortIndexAmongExternals(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))

	const subId = "sub-interleave"
	db := database.GetDB()

	// The high-index inbound is created first, so neither creation order nor a
	// sort that ignored subSortIndex can produce the expected interleaving.
	seed := []struct {
		tag          string
		port         int
		subSortIndex int
		email        string
		uuid         string
	}{
		{"interleave-a", 42201, 3, "a@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf101"},
		{"interleave-b", 42202, 1, "b@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf102"},
	}
	for _, s := range seed {
		settings := fmt.Sprintf(`{"clients": [{"id": %q, "email": %q, "subId": %q, "enable": true}]}`, s.uuid, s.email, subId)
		ib := &model.Inbound{
			UserId:         1,
			Tag:            s.tag,
			Enable:         true,
			Port:           s.port,
			Protocol:       model.VLESS,
			Settings:       settings,
			StreamSettings: `{"network": "tcp", "security": "none"}`,
			SubSortIndex:   s.subSortIndex,
		}
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", s.tag, err)
		}
		client := &model.ClientRecord{Email: s.email, SubID: subId, UUID: s.uuid, Enable: true}
		if err := db.Create(client).Error; err != nil {
			t.Fatalf("seed client %s: %v", s.email, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
			t.Fatalf("seed client_inbound %s: %v", s.email, err)
		}
	}

	// Three separate external clients so the emails probe whether each link
	// stays attributed to its own owner once the list is reordered.
	for i, marker := range []string{"one", "two", "three"} {
		ext := &model.ClientRecord{Email: fmt.Sprintf("e%d@example.com", i+1), SubID: subId, Enable: true}
		if err := db.Create(ext).Error; err != nil {
			t.Fatalf("seed external client %d: %v", i, err)
		}
		link := fmt.Sprintf("vless://00000000-0000-0000-0000-00000000000%d@%s.example.com:443?type=tcp", i+1, marker)
		if err := db.Create(&model.ClientExternalLink{
			ClientId: ext.Id,
			Kind:     model.ExternalLinkKindLink,
			Value:    link,
			Remark:   strings.ToUpper(marker),
		}).Error; err != nil {
			t.Fatalf("seed external link %d: %v", i, err)
		}
	}

	s := NewSubService("")
	links, emails, _, _, err := s.GetSubs(subId, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}

	classify := func(link string) string {
		switch {
		case strings.Contains(link, "a@example.com") || strings.Contains(link, seed[0].uuid):
			return "inbound-a"
		case strings.Contains(link, "b@example.com") || strings.Contains(link, seed[1].uuid):
			return "inbound-b"
		case strings.Contains(link, "one.example.com"):
			return "ext-one"
		case strings.Contains(link, "two.example.com"):
			return "ext-two"
		case strings.Contains(link, "three.example.com"):
			return "ext-three"
		default:
			return "unknown: " + link
		}
	}

	// subSortIndex 1 for b, 3 for a; externals take the slots 2, 4 and 5.
	wantOrder := []string{"inbound-b", "ext-one", "inbound-a", "ext-two", "ext-three"}
	gotOrder := make([]string, 0, len(links))
	for _, link := range links {
		gotOrder = append(gotOrder, classify(link))
	}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("links order = %v, want %v (subSortIndex is the 1-based slot, externals fill the gaps)", gotOrder, wantOrder)
	}

	wantEmails := []string{"b@example.com", "e1@example.com", "a@example.com", "e2@example.com", "e3@example.com"}
	if len(emails) != len(wantEmails) {
		t.Fatalf("emails = %v, want %v (emails are positionally paired with links)", emails, wantEmails)
	}
	for i, email := range wantEmails {
		if emails[i] != email {
			t.Errorf("emails[%d] = %q, want %q (full %v)", i, emails[i], email, emails)
		}
	}
}

// Fails while an inbound whose subSortIndex exceeds the number of links is
// dropped or pushed past the externals: the clamp must keep it as the last
// inbound slot so the lower-indexed inbounds stay ahead of it.
func TestGetSubs_ClampsSubSortIndexPastListEnd(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))

	const subId = "sub-overflow"
	db := database.GetDB()

	seed := []struct {
		port         int
		subSortIndex int
		email        string
	}{
		{42311, 1, "inb1@example.com"},
		{42312, 99, "inb2@example.com"},
	}
	for i, s := range seed {
		uuid := fmt.Sprintf("0d68a695-4be1-4d92-a9c3-8c0f1c2cf3%02d", i)
		settings := fmt.Sprintf(`{"clients": [{"id": %q, "email": %q, "subId": %q, "enable": true}]}`, uuid, s.email, subId)
		ib := &model.Inbound{
			UserId: 1, Tag: fmt.Sprintf("overflow-%d", i), Enable: true, Port: s.port,
			Protocol: model.VLESS, Settings: settings,
			StreamSettings: `{"network": "tcp", "security": "none"}`,
			SubSortIndex:   s.subSortIndex,
		}
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %d: %v", i, err)
		}
		client := &model.ClientRecord{Email: s.email, SubID: subId, UUID: uuid, Enable: true}
		if err := db.Create(client).Error; err != nil {
			t.Fatalf("seed client %d: %v", i, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
			t.Fatalf("seed client_inbound %d: %v", i, err)
		}
	}

	ext := &model.ClientRecord{Email: "ext@example.com", SubID: subId, Enable: true}
	if err := db.Create(ext).Error; err != nil {
		t.Fatalf("seed external client: %v", err)
	}
	if err := db.Create(&model.ClientExternalLink{
		ClientId: ext.Id, Kind: model.ExternalLinkKindLink,
		Value:  "vless://00000000-0000-0000-0000-0000000000ff@overflow.example.com:443?type=tcp",
		Remark: "EXT",
	}).Error; err != nil {
		t.Fatalf("seed external link: %v", err)
	}

	s := NewSubService("")
	links, _, _, _, err := s.GetSubs(subId, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}

	got := make([]string, 0, len(links))
	for _, link := range links {
		switch {
		case strings.Contains(link, "inb1"):
			got = append(got, "inb1")
		case strings.Contains(link, "inb2"):
			got = append(got, "inb2")
		case strings.Contains(link, "overflow.example.com"):
			got = append(got, "ext")
		default:
			got = append(got, "unknown: "+link)
		}
	}
	// Index 99 outruns the three links, so it clamps to slot 3 — behind the
	// external that has no index at all — rather than leaving the list alone.
	want := "inb1,ext,inb2"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %v, want %v (index past the list end clamps to the last inbound slot)", got, want)
	}
}

// The JSON format must place an inbound's configs on the same subSortIndex slot
// the raw format gives its links: an external profile with no index fills the
// slot the inbounds leave free instead of trailing after all of them. It fails
// while GetJson appends external profiles after every inbound config.
func TestGetJson_PositionsInboundsBySubSortIndexAmongExternals(t *testing.T) {
	initSubDB(t)
	db := database.GetDB()

	const subId = "sub-json-interleave"
	seed := []struct {
		tag          string
		port         int
		subSortIndex int
		email        string
		uuid         string
	}{
		{"json-a", 42401, 3, "a@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf401"},
		{"json-b", 42402, 1, "b@example.com", "0d68a695-4be1-4d92-a9c3-8c0f1c2cf402"},
	}
	for _, s := range seed {
		settings := fmt.Sprintf(`{"clients": [{"id": %q, "email": %q, "subId": %q, "enable": true}]}`, s.uuid, s.email, subId)
		ib := &model.Inbound{
			UserId: 1, Tag: s.tag, Enable: true, Port: s.port, Protocol: model.VLESS,
			Settings: settings, StreamSettings: `{"network": "tcp", "security": "none"}`,
			SubSortIndex: s.subSortIndex,
		}
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", s.tag, err)
		}
		client := &model.ClientRecord{Email: s.email, SubID: subId, UUID: s.uuid, Enable: true}
		if err := db.Create(client).Error; err != nil {
			t.Fatalf("seed client %s: %v", s.email, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
			t.Fatalf("seed client_inbound %s: %v", s.email, err)
		}
	}

	for i, marker := range []string{"one", "two", "three"} {
		ext := &model.ClientRecord{Email: fmt.Sprintf("e%d@example.com", i+1), SubID: subId, Enable: true}
		if err := db.Create(ext).Error; err != nil {
			t.Fatalf("seed external client %d: %v", i, err)
		}
		link := fmt.Sprintf("vless://00000000-0000-0000-0000-00000000000%d@%s.example.com:443?type=tcp", i+1, marker)
		if err := db.Create(&model.ClientExternalLink{
			ClientId: ext.Id, Kind: model.ExternalLinkKindLink, Value: link,
			Remark: strings.ToUpper(marker),
		}).Error; err != nil {
			t.Fatalf("seed external link %d: %v", i, err)
		}
	}

	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetJson(subId, "sub.example.com", true)
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var docs []json.RawMessage
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("GetJson must return an array: %v\n%s", err, out)
	}

	classify := func(doc json.RawMessage) string {
		body := string(doc)
		switch {
		case strings.Contains(body, seed[0].uuid):
			return "inb-a"
		case strings.Contains(body, seed[1].uuid):
			return "inb-b"
		case strings.Contains(body, "one.example.com"):
			return "ext-one"
		case strings.Contains(body, "two.example.com"):
			return "ext-two"
		case strings.Contains(body, "three.example.com"):
			return "ext-three"
		default:
			return "unknown: " + body
		}
	}

	want := []string{"inb-b", "ext-one", "inb-a", "ext-two", "ext-three"}
	got := make([]string, 0, len(docs))
	for _, doc := range docs {
		got = append(got, classify(doc))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("JSON order = %v, want %v (same subSortIndex slots as the raw format)", got, want)
	}
}
