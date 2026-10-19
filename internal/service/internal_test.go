package service

import "testing"

func TestFindRepomdLocationHash(t *testing.T) {
	repomd := `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <revision>1700000000</revision>
  <data type="primary">
    <checksum type="sha256">aaaabbbbccccdddd0000111122223333444455556666777788889999aaaabbbb</checksum>
    <open-checksum type="sha256">1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff</open-checksum>
    <location href="repodata/aaaabbbbccccdddd0000111122223333444455556666777788889999aaaabbbb-primary.xml.gz"/>
    <timestamp>1700000000</timestamp>
    <size>100</size>
  </data>
  <data type="filelists">
    <location href="repodata/bbbbccccddddeeee0000111122223333444455556666777788889999aaaa0000-filelists.xml.gz"/>
  </data>
  <data type="other">
    <location href="repodata/ccccddddeeeeffff0000111122223333444455556666777788889999aaaa0000-other.xml"/>
  </data>
</repomd>
`

	cases := []struct {
		kind string
		gz   bool
		want string
	}{
		{"primary", true, "aaaabbbbccccdddd0000111122223333444455556666777788889999aaaabbbb"},
		{"primary", false, ""},
		{"filelists", true, "bbbbccccddddeeee0000111122223333444455556666777788889999aaaa0000"},
		{"filelists", false, ""},
		{"other", false, "ccccddddeeeeffff0000111122223333444455556666777788889999aaaa0000"},
		{"other", true, ""},
	}

	for _, c := range cases {
		if got := findRepomdLocationHash(repomd, c.kind, c.gz); got != c.want {
			t.Errorf("findRepomdLocationHash(%q, gz=%v) = %q, want %q", c.kind, c.gz, got, c.want)
		}
	}
}
