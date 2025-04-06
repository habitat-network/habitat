package types

// Types that need to be shared by many modules in internal/

type HabitatHostname string
type HabitatResolver func(string) HabitatHostname
