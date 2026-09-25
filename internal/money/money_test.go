package money

import "testing"

func TestFormat(t *testing.T) {
	const nbsp = " "
	cases := []struct {
		m    Money
		lang string
		want string
	}{
		{FromMajor(20000, KZT), "ru", "20" + nbsp + "000" + nbsp + "₸"},
		{FromMajor(50000, KZT), "kk", "50" + nbsp + "000" + nbsp + "₸"},
		{FromMajor(30000, KZT), "en", "₸30,000"},
		{FromMajor(999, KZT), "ru", "999" + nbsp + "₸"},
		{FromMajor(1234567, KZT), "ru", "1" + nbsp + "234" + nbsp + "567" + nbsp + "₸"},
		{Money{Minor: 199050, Currency: RUB}, "ru", "1" + nbsp + "990,50" + nbsp + "₽"},
		{Money{Minor: 105, Currency: KZT}, "en", "₸1.05"},
		{Money{Minor: -250000, Currency: KZT}, "ru", "−2" + nbsp + "500" + nbsp + "₸"},
		{Money{Currency: KZT}, "ru", "0" + nbsp + "₸"},
	}
	for _, tc := range cases {
		if got := tc.m.Format(tc.lang); got != tc.want {
			t.Errorf("Format(%+v, %s) = %q, ожидалось %q", tc.m, tc.lang, got, tc.want)
		}
	}
}

func TestFromMajor(t *testing.T) {
	if got := FromMajor(20000, KZT); got.Minor != 2000000 || got.Currency != KZT {
		t.Errorf("FromMajor(20000) = %+v", got)
	}
}
