// Package nimegami is the leaf scraper for nimegami.id (Indonesian-subtitled
// anime). WordPress; plain HTTP, no challenge. Unlike the other sources, one
// post holds a whole season, so an episode is addressed as
// <anime page URL>#play_eps_<n>.
//
//	GET  /?s=<query>                    → <article> cards (HTML)
//	GET  /<slug>-sub-indo/              → every episode as
//	                                      li.select-eps[data=base64 JSON]
//	                                      [{format:"720p", url:[…]}, …]
//	GET  stordl…/streaming/<id>         → STREAM_URL_API player endpoint
//	GET  …?action=stream-url&id=<id>    → {ok:true, url:"…mp4"}
//	GET  dl.berkasdrive.com/streaming/  → legacy <source src="…mp4">
//
// The file URL's `?filename=` query is dropped: it carries unescaped spaces
// and the CDN serves the bare path. The host may rotate; only the const in
// client.go changes.
package nimegami
