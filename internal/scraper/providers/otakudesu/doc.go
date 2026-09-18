// Package otakudesu is the leaf scraper for otakudesu (Indonesian-subtitled
// anime). The site is WordPress; the chain is plain HTTP, no challenge:
//
//	GET  /?s=<query>&post_type=anime           → anime cards (HTML)
//	GET  /anime/<slug>/                         → episode list (HTML)
//	GET  /episode/<slug>/                       → mirror list + ajax action hashes
//	POST /wp-admin/admin-ajax.php {action}      → {"data":"<nonce>"}
//	POST /wp-admin/admin-ajax.php {id,i,q,nonce,action} → {"data":base64(<iframe>)}
//	GET  <desustream embed>                     → direct .mp4 or Blogger token URL
//
// The episode page's download section is tried first: its Pdrain links
// (link.desustream.com/?id=… → 302 → pixeldrain.com/u/<id>) become direct
// /api/file/<id> URLs and reach 1080p. Then the streaming mirrors: only
// desustream-hosted embeds and Blogger iframes are resolved; the player already
// turns a Blogger video.g?token URL into a playable file. The host rotates
// often (otakudesu.cloud → .best → .blog in 2026); only the const below changes.
package otakudesu
