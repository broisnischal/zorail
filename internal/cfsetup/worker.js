// Zorail ingest Worker — deployed by `zmail setup`.
//
// Cloudflare Email Routing invokes email() once per recipient. We stream the
// raw RFC822 message straight to the Zorail server's /api/ingest endpoint
// (reached over a Cloudflare Tunnel), authenticated with the server's API
// token. The envelope recipient + sender ride along as query params, matching
// the ingest endpoint's raw mode.
//
// Bindings (set during deploy): INGEST_URL, INGEST_TOKEN.
export default {
  async email(message, env, ctx) {
    // message.raw is a single-use stream — buffer it once.
    const raw = await new Response(message.raw).arrayBuffer();

    const url = new URL(env.INGEST_URL);
    url.searchParams.set("rcpt", message.to);
    if (message.from) url.searchParams.set("env_from", message.from);

    let res;
    try {
      res = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "message/rfc822",
          "Authorization": "Bearer " + env.INGEST_TOKEN,
        },
        body: raw,
      });
    } catch (err) {
      // Network/tunnel down — reject with a transient error so the sending MTA
      // retries later rather than losing the mail.
      message.setReject("temporary ingest failure: " + err);
      return;
    }

    if (!res.ok) {
      const body = await res.text().catch(() => "");
      message.setReject("ingest rejected (" + res.status + "): " + body.slice(0, 120));
    }
  },
};
