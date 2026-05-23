package pow

type ypScratch struct {
	v    []ypBlock
	xy   []ypBlock
	smem []ypBlock
}

type PooledYespower struct {
	hasher  YespowerHasher
	scratch ypScratch
}

func NewPooledYespower(personalization string) *PooledYespower {
	return &PooledYespower{
		hasher: YespowerHasher{Personalization: personalization},
	}
}

func (p *PooledYespower) Personalization() string {
	return p.hasher.Personalization
}
