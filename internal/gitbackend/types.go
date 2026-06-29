package gitbackend

// the all-zero object id git uses to denote a missing ref
// its zero in before, if it was created after
// or its zero after, if it was deleted before
const zeroSHA = "0000000000000000000000000000000000000000"

type RefChange struct {
	Ref    string
	OldSHA string
	NewSHA string
}
