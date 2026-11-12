package mdplain

import "testing"

// What new URL() answered for these hosts (Node 24, the WHATWG parser the browsers share).
func TestHostValidAgreesWithTheURLStandard(t *testing.T) {
	valid := []string{"a..b/", "x_y.example", "a!$&'()*+,;=~{}.x/", "1.2.3.4/", "0x7f.1/", "a.1e/", "u:p@h.x/", "@h.x/",
		"h.x:/", "h.x:0080/", ".x/", "x./", "./", "-x-.y/", "ab_.cd/", "a.b_/", "0/", "1.2.3/", "4294967295/", "1.2.3.4./",
		"a@b@c/", "A.B/", "{x}/", "x.y/?a", "x#y", "x?y", "a.b.c.d.e.f/", "0x/", "0xg/", "a.0xg/", "1.0x/", "a.b./",
		"a..b..c", "..", "a...", "0.0.0.0", "00/", "0x00000000001/", "1.2.0x3/", "a.b@c.d/", "x.y:65535", "-/", "_/"}
	invalid := []string{"1.2.3.999/", "1.2.3.4.5/", "a.0x/", "a.08/", "h.x@/", "h.x:65536/", "a|b/", "a^b/", "09/",
		"4294967296/", "a..1/", "1..2/", "a:b@/", "a.b:12a/", "x:1:2/", "12345678901234567890/",
		"[[zq]]/", "[::1]x/", "[/", "[zz]/", "e.example[/"}
	unknown := []string{"a%41b/", "xn--abc/", "xn--caf-dma.example/", "x.XN--y/", "[::1]/", "[::1]:80/", "a@[::1]/", "[1:2]/", "caf\u00e9.example/"}
	for _, host := range valid {
		if sure, ok := hostValid(host); !sure || !ok {
			t.Errorf("%q: sure %v valid %v, want a valid host", host, sure, ok)
		}
	}
	for _, host := range invalid {
		if sure, ok := hostValid(host); !sure || ok {
			t.Errorf("%q: sure %v valid %v, want a refused host", host, sure, ok)
		}
	}
	for _, host := range unknown {
		if sure, _ := hostValid(host); sure {
			t.Errorf("%q: this code claims to know the browser's answer", host)
		}
	}
}
