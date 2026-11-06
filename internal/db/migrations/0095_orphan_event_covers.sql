-- Deleting a channel cascaded its events away but only nulled media.channel_id,
-- so the covers of those events stayed as channel-less media rows that every
-- member could fetch and nothing ever removed. The delete now takes the covers
-- with the channel; this removes the ones left behind. A day's grace keeps a
-- cover that was uploaded a moment ago and is not attached to its event yet.
-- The objects stay in storage until the orphan sweep (now covering covers/).
DELETE FROM media m
WHERE m.s3_key LIKE 'covers/%'
  AND m.created_at < NOW() - INTERVAL '1 day'
  AND NOT EXISTS (SELECT 1 FROM events e WHERE e.cover_media_id = m.id);
