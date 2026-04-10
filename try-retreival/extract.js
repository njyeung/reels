const fs = require("fs");

const files = ["test5.json", "test2.json", "test3.json", "test4.json"];

for (const file of files) {
  const data = JSON.parse(fs.readFileSync(file, "utf8"));
  const thread = data.data.get_slide_thread_nullable.as_ig_direct_thread;

  const viewerFbid = thread.viewer.interop_messaging_user_fbid;
  const receipts = thread.slide_read_receipts || [];
  const viewerReceipt = receipts.find((r) => r.participant_fbid === viewerFbid);
  const watermark = viewerReceipt
    ? parseInt(viewerReceipt.watermark_timestamp_ms)
    : 0;

  const edges = thread.slide_messages?.edges || [];

  // Filter: unseen reel messages not sent by the viewer
  const unseenReels = edges
    .map((e) => e.node)
    .filter((msg) => {
      if (msg.sender_fbid === viewerFbid) return false;
      if (msg.content_type !== "MESSAGE_INLINE_SHARE") return false;
      const decoration =
        msg.content?.xma?.xmaPreviewImage?.preview_image_decoration_type ||
        msg.content?.xma?.preview_image?.preview_image_decoration_type;
      if (decoration !== "REEL") return false;
      return parseInt(msg.timestamp_ms) > watermark;
    })
    .map((msg) => ({
      from: msg.sender?.user_dict?.username,
      reel_author: msg.content.xma.xmaHeaderTitle,
      reel_url: msg.content.xma.target_url,
      reel_id: msg.content.xma.target_id,
      timestamp: new Date(parseInt(msg.timestamp_ms)).toISOString(),
    }));

  console.log(`\n=== ${thread.thread_title} (${file}) ===`);
  console.log(`Viewer watermark: ${new Date(watermark).toISOString()}`);
  console.log(`Total messages: ${edges.length}`);
  console.log(`Unseen reels from others: ${unseenReels.length}`);
  for (const r of unseenReels) {
    console.log(
      `  - @${r.reel_author} reel (sent by @${r.from}) at ${r.timestamp}`
    );
    console.log(`    ID: ${r.reel_id}`);
    console.log(`    ${r.reel_url}`);
  }
}
