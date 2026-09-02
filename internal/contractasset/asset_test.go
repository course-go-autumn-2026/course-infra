package contractasset

import "testing"

func TestValidateLinkedDevelopmentBuild(t *testing.T) {
	if err := ValidateLinked(); err != nil {
		t.Fatal(err)
	}
}
