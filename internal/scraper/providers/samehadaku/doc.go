// Package samehadaku is the leaf scraper for samehadaku (Indonesian-subtitled
// anime). WordPress "eastplay" theme; plain HTTP, no challenge:
//
//	GET  /?s=<query>                                    → anime cards (HTML)
//	GET  /anime/<slug>/                                 → episode list (HTML)
//	GET  /<slug>-episode-<n>/                           → server list (data-post/nume/type)
//	POST /wp-admin/admin-ajax.php action=player_ajax    → <iframe src=…>
//
// Servers resolved: Pixeldrain (pixeldrain.com/u/<id> → /api/file/<id>, a
// direct mp4 per quality) and Blogspot (a Blogger video.g?token URL the player
// already knows how to unwrap). Kraken/Vidhide/wibufile need per-host
// scraping and are skipped. The host rotates (samehadaku.how → v2.…); only
// the const below changes.
package samehadaku
