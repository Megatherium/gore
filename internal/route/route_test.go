package route_test

import (
	"testing"

	"github.com/leoherzog/reprox/internal/route"
)

func TestParseRoute(t *testing.T) {
	t.Run("basic parsing", func(t *testing.T) {
		r := route.ParseRoute("/owner/repo/some/path")
		if r.Owner != "owner" || r.Repo != "repo" {
			t.Errorf("got owner=%s, repo=%s; want owner=owner, repo=repo", r.Owner, r.Repo)
		}

		empty := route.ParseRoute("/")
		if empty.Owner != "" || empty.Repo != "" || empty.Type != route.TypeUnknown {
			t.Errorf("empty path failed: got %+v", empty)
		}

		onlyOwner := route.ParseRoute("/owner")
		if onlyOwner.Owner != "owner" || onlyOwner.Repo != "" {
			t.Errorf("only owner failed: got %+v", onlyOwner)
		}

		unknown := route.ParseRoute("/owner/repo/unknown/path")
		if unknown.Type != route.TypeUnknown {
			t.Errorf("expected unknown type, got %s", unknown.Type)
		}

		if unknown.Distribution != "stable" || unknown.Component != "main" || unknown.Architecture != "amd64" {
			t.Errorf("defaults not set correctly: %+v", unknown)
		}
	})

	t.Run("public-key route", func(t *testing.T) {
		r := route.ParseRoute("/joshuar/go-hass-agent/public.key")
		if r.Type != route.TypePublicKey || r.Owner != "joshuar" || r.Repo != "go-hass-agent" {
			t.Errorf("public-key route failed: %+v", r)
		}
	})

	t.Run("APT dists routes", func(t *testing.T) {
		r1 := route.ParseRoute("/owner/repo/dists/stable/InRelease")
		if r1.Type != route.TypeInRelease || r1.Distribution != "stable" {
			t.Errorf("InRelease failed: %+v", r1)
		}

		r2 := route.ParseRoute("/owner/repo/dists/focal/Release")
		if r2.Type != route.TypeRelease || r2.Distribution != "focal" {
			t.Errorf("Release failed: %+v", r2)
		}

		r3 := route.ParseRoute("/owner/repo/dists/jammy/Release.gpg")
		if r3.Type != route.TypeReleaseGpg || r3.Distribution != "jammy" {
			t.Errorf("Release.gpg failed: %+v", r3)
		}

		r4 := route.ParseRoute("/owner/repo/dists/stable/main/binary-amd64/Packages")
		if r4.Type != route.TypePackages || r4.Component != "main" || r4.Architecture != "amd64" {
			t.Errorf("Packages failed: %+v", r4)
		}

		r5 := route.ParseRoute("/owner/repo/dists/stable/main/binary-arm64/Packages.gz")
		if r5.Type != route.TypePackagesGz || r5.Architecture != "arm64" {
			t.Errorf("Packages.gz failed: %+v", r5)
		}
	})

	t.Run("APT by-hash routes", func(t *testing.T) {
		hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		r := route.ParseRoute("/owner/repo/dists/stable/main/binary-amd64/by-hash/SHA256/" + hash)
		if r.Type != route.TypeByHash || r.HashType != "SHA256" || r.Hash != hash {
			t.Errorf("by-hash failed: %+v", r)
		}
	})

	t.Run("APT pool routes", func(t *testing.T) {
		r := route.ParseRoute("/owner/repo/pool/main/h/hello/hello_1.0.0_amd64.deb")
		if r.Type != route.TypeBinary || r.Filename != "hello_1.0.0_amd64.deb" {
			t.Errorf("pool route failed: %+v", r)
		}
	})

	t.Run("RPM routes", func(t *testing.T) {
		r1 := route.ParseRoute("/owner/repo/repodata/repomd.xml")
		if r1.Type != route.TypeRepomd {
			t.Errorf("repomd failed: %+v", r1)
		}

		r2 := route.ParseRoute("/owner/repo/repodata/repomd.xml.asc")
		if r2.Type != route.TypeRepomdAsc {
			t.Errorf("repomd.xml.asc failed: %+v", r2)
		}

		r3 := route.ParseRoute("/owner/repo/repodata/primary.xml.gz")
		if r3.Type != route.TypePrimaryGz {
			t.Errorf("primary.xml.gz failed: %+v", r3)
		}

		hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		r4 := route.ParseRoute("/owner/repo/repodata/" + hash + "-primary.xml.gz")
		if r4.Type != route.TypePrimaryGz || r4.Hash != hash {
			t.Errorf("hashed primary failed: %+v", r4)
		}

		r5 := route.ParseRoute("/owner/repo/Packages/hello-1.0.0-1.x86_64.rpm")
		if r5.Type != route.TypeRpmBinary || r5.Filename != "hello-1.0.0-1.x86_64.rpm" {
			t.Errorf("RPM binary failed: %+v", r5)
		}
	})

	t.Run("prerelease routes", func(t *testing.T) {
		r1 := route.ParseRoute("/owner/repo/prerelease/dists/stable/InRelease")
		if r1.ReleaseVariant != "prerelease" || r1.Type != route.TypeInRelease {
			t.Errorf("prerelease InRelease failed: %+v", r1)
		}

		r2 := route.ParseRoute("/owner/repo/prerelease/public.key")
		if r2.ReleaseVariant != "prerelease" || r2.Type != route.TypePublicKey {
			t.Errorf("prerelease public.key failed: %+v", r2)
		}

		r3 := route.ParseRoute("/owner/repo/prerelease/repodata/repomd.xml")
		if r3.ReleaseVariant != "prerelease" || r3.Type != route.TypeRepomd {
			t.Errorf("prerelease repomd failed: %+v", r3)
		}

		r4 := route.ParseRoute("/owner/repo/prerelease/pool/main/h/hello/hello_1.0.0_amd64.deb")
		if r4.ReleaseVariant != "prerelease" || r4.Type != route.TypeBinary || r4.Filename != "hello_1.0.0_amd64.deb" {
			t.Errorf("prerelease deb failed: %+v", r4)
		}
	})
}
