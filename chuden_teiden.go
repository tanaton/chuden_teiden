package main

import (
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"text/template"
	"time"
)

const (
	ServerAddr   = ":80"
	ChudenXmlURL = "http://teiden.chuden.jp/p/resource/xml/"
	ShortForm    = "2006/01/02 15:04"
	HttpDir      = "./public_html"
	TeidenCycle  = 5 * time.Minute
)

type CustomTime time.Time

type TeidenRiyu struct {
	Code   string `xml:"teiden_riyu_c"` // なんかJSの関数が入ってる事があるので
	Naiyou string `xml:"teiden_riyu_n"`
}

type KanrenMotoEigyosho struct {
	Code int    `xml:"kanren_moto_eigyosho_c"`
	Name string `xml:"kanren_moto_eigyosho_n"`
}

type ChomeiInfo struct {
	Cond     int    `xml:"teisoden_cond"`
	Address1 string `xml:"address1_n"`
	Address2 string `xml:"address2_n,omitempty"`
	Address3 string `xml:"address3_n,omitempty"`
	Address4 string `xml:"address4_n,omitempty"`
}

type KoshoKenmei struct {
	Type               int                 `xml:"type,attr"`
	No                 string              `xml:"kenmei_no"`
	Flag               int                 `xml:"ijo_flg,omitempty"`
	Cond               int                 `xml:"kenmei_cond"`
	TeidenHasseiDate   *CustomTime         `xml:"teiden_hassei_d,omitempty"`
	ZensoDate          *CustomTime         `xml:"zenso_d,omitempty"`
	ChomeiInfo         []*ChomeiInfo       `xml:"chomei_info,omitempty"`
	HasseijiTeidenKosu int                 `xml:"hasseiji_teiden_kosu,omitempty"`
	GenzaiTeidenKosu   int                 `xml:"genzai_teiden_kosu"`
	FukkyuMikomiDate   *CustomTime         `xml:"fukkyu_mikomi_d,omitempty"`
	KanrenMotoEigyosho *KanrenMotoEigyosho `xml:"kanren_moto_eigyosho,omitempty"`
	TeidenRiyu         *TeidenRiyu         `xml:"teiden_riyu,omitempty"`
}

type Eigyosho struct {
	Code int    `xml:"eigyosho_c"`
	Name string `xml:"eigyosho_n"`
}

type TeidenInfo struct {
	DataMakeDate *CustomTime    `xml:"data_make_d,omitempty"`
	Eigyosho     *Eigyosho      `xml:"eigyosho,omitempty"`
	KoshoKenmei  []*KoshoKenmei `xml:"kosho_kenmei,omitempty"`
}

type chudenTeidenHandle struct {
	sync.RWMutex
	file http.Handler
	date time.Time
	til  []*TeidenInfo
}

type Office struct {
	Code int
	Name string
}

var JST = time.FixedZone("Asia/Tokyo", 9*60*60)
var Tmpl = template.Must(template.ParseFiles("ken.tmpl", "shi.tmpl", "ban.tmpl"))
var OfficeList = []int64{
	250, // 浜松
	255, // 細江
	256, // 浜北
	240, // 掛川
	241, // 磐田
}

func main() {
	cth := &chudenTeidenHandle{
		file: http.FileServer(http.Dir(HttpDir)),
	}
	cth.teiden(time.Now())
	go func() {
		c := time.Tick(TeidenCycle)
		for now := range c {
			cth.teiden(now)
		}
	}()
	h := &http.Server{
		Addr:    ServerAddr,
		Handler: cth,
	}
	log.Fatal(h.ListenAndServe())
}

func (cth *chudenTeidenHandle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := path.Clean(r.URL.Path)
	if p == "/" || p == "/index.html" {
		// トップページが無いのでリダイレクト
		http.Redirect(w, r, "/静岡県", http.StatusMovedPermanently)
	} else if strings.Index(p, "/") == 0 {
		q := strings.Split(p[1:], "/")
		switch len(q) {
		case 0:
			ken := cth.ken("静岡県")
			Tmpl.ExecuteTemplate(w, "ken.tmpl", &ken)
		case 1:
			ken := cth.ken(q[0])
			Tmpl.ExecuteTemplate(w, "ken.tmpl", &ken)
		case 2:
			shi := cth.shi(q[0], q[1])
			Tmpl.ExecuteTemplate(w, "shi.tmpl", &shi)
		case 3, 4:
			ban := cth.banchi(q)
			Tmpl.ExecuteTemplate(w, "ban.tmpl", &ban)
		default:
			http.NotFound(w, r)
		}
	} else {
		// ファイルサーバにお任せ
		cth.file.ServeHTTP(w, r)
	}
}

func (cth *chudenTeidenHandle) teiden(now time.Time) error {
	til := []*TeidenInfo{}
	for _, code := range OfficeList {
		ti, err := teidenGet(code, now)
		if err != nil {
			continue
		}
		til = append(til, ti)
	}
	cth.Lock()
	cth.til = til
	cth.date = now
	cth.Unlock()
	return nil
}

func teidenGet(code int64, now time.Time) (*TeidenInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s%d.xml?%d", ChudenXmlURL, code, now.Unix()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	ti := &TeidenInfo{}
	err = xml.NewDecoder(resp.Body).Decode(ti)
	if err != nil {
		return nil, err
	}
	return ti, nil
}

type Count struct {
	Hassei int
	Genzai int
}
type Item struct {
	ChomeiInfo         []*ChomeiInfo
	HasseijiTeidenKosu int
	GenzaiTeidenKosu   int
	TeidenHasseiDate   string
	ZensoDate          string
	FukkyuMikomiDate   string
}
type Ken struct {
	Count
	Date string
	Ken  string
	Shi  map[string]struct{}
}
type Shi struct {
	Count
	Date string
	Ken  string
	Shi  string
	Cho  map[string]struct{}
}
type Loc struct {
	Count
	Date       string
	Ken        string
	Shi        string
	Cho        string
	Banchi     string
	BanchiList map[string]struct{}
	Item       []Item
}

func (cth *chudenTeidenHandle) ken(k string) Ken {
	cth.RLock()
	defer cth.RUnlock()
	ken := Ken{
		Date: cth.date.Format(ShortForm),
		Ken:  k,
		Shi:  map[string]struct{}{},
	}
	for _, ti := range cth.til {
		for _, kosho := range ti.KoshoKenmei {
			found := false
			var chomei *ChomeiInfo
			for _, chomei = range kosho.ChomeiInfo {
				if chomei.Address1 == k {
					found = true
					if chomei.Address2 != "" {
						ken.Shi[chomei.Address2] = struct{}{}
					}
				}
			}
			if found {
				ken.Hassei += kosho.HasseijiTeidenKosu
				ken.Genzai += kosho.GenzaiTeidenKosu
			}
		}
	}
	return ken
}

func (cth *chudenTeidenHandle) shi(k, s string) Shi {
	cth.RLock()
	defer cth.RUnlock()
	shi := Shi{
		Date: cth.date.Format(ShortForm),
		Ken:  k,
		Shi:  s,
		Cho:  map[string]struct{}{},
	}
	for _, ti := range cth.til {
		for _, kosho := range ti.KoshoKenmei {
			found := false
			var chomei *ChomeiInfo
			for _, chomei = range kosho.ChomeiInfo {
				if chomei.Address1 == k && chomei.Address2 == s {
					found = true
					if chomei.Address3 != "" {
						shi.Cho[chomei.Address3] = struct{}{}
					}
				}
			}
			if found {
				shi.Hassei += kosho.HasseijiTeidenKosu
				shi.Genzai += kosho.GenzaiTeidenKosu
			}
		}
	}
	return shi
}

func (cth *chudenTeidenHandle) banchi(q []string) Loc {
	cth.RLock()
	defer cth.RUnlock()
	loc := Loc{
		Date:       cth.date.Format(ShortForm),
		Ken:        q[0],
		Shi:        q[1],
		Cho:        q[2],
		BanchiList: map[string]struct{}{},
		Item:       []Item{},
	}
	for _, ti := range cth.til {
		for _, kosho := range ti.KoshoKenmei {
			found := false
			var chomei *ChomeiInfo
			for _, chomei = range kosho.ChomeiInfo {
				flag := false
				switch len(q) {
				case 3:
					if chomei.Address1 == q[0] && chomei.Address2 == q[1] && chomei.Address3 == q[2] {
						flag = true
						if chomei.Address4 != "" {
							loc.BanchiList[chomei.Address4] = struct{}{}
						}
					}
				default:
					loc.Banchi = q[3]
					if chomei.Address1 == q[0] && chomei.Address2 == q[1] && chomei.Address3 == q[2] && chomei.Address4 == q[3] {
						flag = true
					}
				}
				if flag {
					found = true
				}
			}
			if found {
				loc.Hassei += kosho.HasseijiTeidenKosu
				loc.Genzai += kosho.GenzaiTeidenKosu

				var TeidenHasseiDate string
				var ZensoDate string
				var FukkyuMikomiDate string
				if kosho.TeidenHasseiDate != nil {
					TeidenHasseiDate = time.Time(*kosho.TeidenHasseiDate).Format(ShortForm)
				}
				if kosho.ZensoDate != nil {
					ZensoDate = time.Time(*kosho.ZensoDate).Format(ShortForm)
				}
				if kosho.FukkyuMikomiDate != nil {
					FukkyuMikomiDate = time.Time(*kosho.FukkyuMikomiDate).Format(ShortForm)
				}
				loc.Item = append(loc.Item, Item{
					ChomeiInfo:         kosho.ChomeiInfo,
					HasseijiTeidenKosu: kosho.HasseijiTeidenKosu,
					GenzaiTeidenKosu:   kosho.GenzaiTeidenKosu,
					TeidenHasseiDate:   TeidenHasseiDate,
					ZensoDate:          ZensoDate,
					FukkyuMikomiDate:   FukkyuMikomiDate,
				})
			}
		}
	}
	return loc
}

// https://stackoverflow.com/questions/17301149/golang-xml-unmarshal-and-time-time-fields
func (c *CustomTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	xerr := d.DecodeElement(&v, &start)
	if xerr != nil {
		return xerr
	}
	parse, err := time.ParseInLocation(ShortForm, v, JST)
	if err != nil {
		return err
	}
	*c = CustomTime(parse)
	return nil
}

func (c *CustomTime) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	s := time.Time(*c).Format(ShortForm)
	return e.EncodeElement(s, start)
}
