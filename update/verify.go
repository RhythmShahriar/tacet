// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// The signer every tacet release must come from: tacet's tag, built by
// the shared release workflow on GitHub Actions.
const (
	Issuer     = "https://token.actions.githubusercontent.com"
	Workflow   = "https://github.com/RhythmShahriar/.github/.github/workflows/release-go.yml@refs/heads/master"
	SourceRepo = "https://github.com/" + Repo
)

// Verifier checks that checksums.txt was signed for the release tag.
type Verifier interface {
	Verify(ctx context.Context, checksums, bundle []byte, tag string) error
}

// ErrSignature means the release's signature or signer didn't check out.
var ErrSignature = errors.New("release signature does not verify")

// Sigstore verifies a Sigstore bundle against the public-good trusted
// root, fetched through TUF from the root sigstore-go embeds.
type Sigstore struct {
	// CacheDir keeps the TUF metadata; empty means Dir()/tuf.
	CacheDir string
}

// Verify implements Verifier. The certificate must chain to Fulcio, the
// signature must be in the Rekor log with a signed timestamp and the
// certificate must name the release workflow, tacet's repository and
// exactly this tag.
func (s Sigstore) Verify(_ context.Context, checksums, bundleJSON []byte, tag string) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("%w: reading the bundle: %w", ErrSignature, err)
	}
	tr, err := s.trustedRoot()
	if err != nil {
		return fmt.Errorf("loading the Sigstore trusted root: %w", err)
	}
	v, err := verify.NewVerifier(tr,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1))
	if err != nil {
		return err
	}
	san, err := verify.NewSANMatcher(Workflow, "")
	if err != nil {
		return err
	}
	iss, err := verify.NewIssuerMatcher(Issuer, "")
	if err != nil {
		return err
	}
	id, err := verify.NewCertificateIdentity(san, iss, certificate.Extensions{
		SourceRepositoryURI: SourceRepo,
		SourceRepositoryRef: "refs/tags/" + tag,
	})
	if err != nil {
		return err
	}
	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(checksums)), verify.WithCertificateIdentity(id))
	if _, err := v.Verify(&b, policy); err != nil {
		return fmt.Errorf("%w: %w", ErrSignature, err)
	}
	return nil
}

// trustedRoot fetches trusted_root.json through TUF, reusing metadata
// cached in the last day. When the TUF mirror can't be reached it falls
// back to the cached copy; with no cache it fails, so nothing installs.
func (s Sigstore) trustedRoot() (*root.TrustedRoot, error) {
	dir := s.CacheDir
	if dir == "" {
		dir = filepath.Join(Dir(), "tuf")
	}
	opts := tuf.DefaultOptions().WithCachePath(dir).WithCacheValidity(1)
	tr, err := root.FetchTrustedRootWithOptions(opts)
	if err == nil {
		return tr, nil
	}
	if tr, cerr := root.FetchTrustedRootWithOptions(tuf.DefaultOptions().WithCachePath(dir).WithForceCache()); cerr == nil {
		return tr, nil
	}
	return nil, err
}
