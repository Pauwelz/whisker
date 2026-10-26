-- Drop stored thumbnails above 256 KiB (whatsapp.MaxThumbnailBytes), and their links. Frames carry no large data.
UPDATE media SET thumbnail = x'', thumb_direct_path = '' WHERE length(thumbnail) > 262144;
