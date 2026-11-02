package geodata

import "strconv"

func build() map[int]string {
	t := make(map[int]string)
	for i := 0; i < 100000; i++ {
		t[i] = "r" + strconv.Itoa(i%977)
	}
	return t
}
