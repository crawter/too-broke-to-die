// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

var (
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	identityPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Codec binds revision validation to the actual campaign and explicitly
// compatible content identities. Construction never loads files or devices.
type Codec struct {
	campaign   *campaign.Campaign
	current    Compatibility
	compatible map[ContentIdentity]bool
	deeds      map[content.DeedID]bool
}

// NewCodec constructs a codec. Compatible identities are explicit declarations
// of unchanged domain rules, not automatic migrations or replacement IDs.
func NewCodec(ctx context.Context, service *campaign.Campaign, manifest *content.Campaign, current Compatibility, compatible []ContentIdentity) (_ *Codec, err error) {
	defer mon.Task()(&ctx)(&err)
	if service == nil || manifest == nil || !validCompatibility(current) {
		return nil, fmt.Errorf("invalid codec configuration")
	}
	codec := &Codec{campaign: service, current: current, compatible: make(map[ContentIdentity]bool), deeds: make(map[content.DeedID]bool)}
	codec.compatible[contentIdentity(current)] = true
	for _, identity := range compatible {
		if !validText(identity.ContentVersion) || !digestPattern.MatchString(identity.ManifestDigest) {
			return nil, fmt.Errorf("invalid compatible content identity")
		}
		codec.compatible[identity] = true
	}
	for _, deed := range manifest.Deeds(ctx) {
		codec.deeds[deed.Deed.ID] = true
	}
	return codec, nil
}

func validText(value string) bool {
	return len(value) <= 1024 && utf8.ValidString(value) && strings.TrimSpace(value) != ""
}

func validCompatibility(value Compatibility) bool {
	return validText(value.ProductVersion) && validText(value.ContentVersion) && digestPattern.MatchString(value.ManifestDigest)
}

func contentIdentity(value Compatibility) ContentIdentity {
	return ContentIdentity{ContentVersion: value.ContentVersion, ManifestDigest: value.ManifestDigest, RequiredMigration: value.RequiredMigration}
}

func validCampaignID(id string) bool {
	return identityPattern.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

func (codec *Codec) validateGuidance(ctx context.Context, guidance Guidance) (err error) {
	defer mon.Task()(&ctx)(&err)
	if guidance.Tracked != "" && !codec.deeds[guidance.Tracked] {
		return fmt.Errorf("unknown tracked ticket")
	}
	seen := make(map[content.PresentationID]bool)
	for _, id := range guidance.Tutorials {
		switch id {
		case "TUT-MOV", "TUT-INT", "TUT-LED", "TUT-CBT", "TUT-CRD", "TUT-LIM", "TUT-UTL", "TUT-FEC", "TUT-REC", "TUT-BOSS", "TUT-SOURCE":
		default:
			return fmt.Errorf("unknown tutorial identity")
		}
		if seen[id] {
			return fmt.Errorf("duplicate tutorial identity")
		}
		seen[id] = true
	}
	return nil
}

func validateMetadata(metadata Metadata) error {
	_, offset := metadata.LastWriteUTC.Zone()
	if metadata.LastWriteUTC.IsZero() || offset != 0 || len(metadata.BuildDescription) > 1024 {
		return fmt.Errorf("invalid non-authoritative metadata")
	}
	return nil
}
