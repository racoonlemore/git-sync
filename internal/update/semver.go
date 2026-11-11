package update

import (
	"strconv"
	"strings"
)

type semver struct {
	major, minor, patch int
	pre                 []string
}

// parseSemver aceita "1.2.3", "v1.2.3", "1.2.3-rc.1" e "1.2.3+build".
// Devolve ok=false para qualquer outra coisa (ex.: "dev").
func parseSemver(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return semver{}, false
		}
		n[i] = x
	}
	s := semver{major: n[0], minor: n[1], patch: n[2]}
	if hasPre {
		s.pre = strings.Split(pre, ".")
	}
	return s, true
}

// compare devolve -1, 0 ou 1, com a precedência do semver.org (uma versão
// com pré-release vem antes da mesma versão sem pré-release).
func compare(x, y semver) int {
	for _, d := range []int{x.major - y.major, x.minor - y.minor, x.patch - y.patch} {
		if d != 0 {
			return sign(d)
		}
	}
	switch {
	case len(x.pre) == 0 && len(y.pre) == 0:
		return 0
	case len(x.pre) == 0:
		return 1
	case len(y.pre) == 0:
		return -1
	}
	for i := 0; i < len(x.pre) && i < len(y.pre); i++ {
		if x.pre[i] == y.pre[i] {
			continue
		}
		xi, xErr := strconv.Atoi(x.pre[i])
		yi, yErr := strconv.Atoi(y.pre[i])
		switch {
		case xErr == nil && yErr == nil:
			return sign(xi - yi)
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		}
		return strings.Compare(x.pre[i], y.pre[i])
	}
	return sign(len(x.pre) - len(y.pre))
}

func sign(d int) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}
