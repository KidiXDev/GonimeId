// Package samehadaku is the leaf scraper for samehadaku (Indonesian-subtitled
// anime). WordPress "eastplay" theme; plain HTTP, no challenge:
//
//	GET  /?s=<query>                                    → anime cards (HTML)
//	GET  /anime/<slug>/                                 → episode list (HTML)
//	GET  /<slug>-episode-<n>/                           → server list (data-post/nume/type)
//	POST /wp-admin/admin-ajax.php action=player_ajax    → <iframe src=…>
//
// Servers resolved: Wibufile's per-quality player streams first, then
// Pixeldrain download links and Blogspot as fallbacks. Mega is skipped. The
// host rotates (samehadaku.how → v2.…); only the const below changes.
package samehadaku
