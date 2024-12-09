#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <arpa/inet.h>
#include <unistd.h>

#include <libavcodec/avcodec.h>
#include <libswscale/swscale.h>

static bool raw = false;
static bool alpha = false;
static unsigned char *video_packet = NULL;
static AVCodecParserContext *parser = NULL;
static AVCodecContext *codec_ctx = NULL;
static struct SwsContext *sws_ctx = NULL;
static AVFrame *frame = NULL;
static AVPacket *packet = NULL;
static uint32_t initial_width = 0;
static uint32_t initial_height = 0;
static uint32_t frame_width = 0;
static uint32_t frame_height = 0;
static size_t frame_size = 0;
static unsigned char *frame_data = NULL;

static const AVCodec *get_decoder(char **argv) {
  switch (strtoul(argv[1], NULL, 10)) {
  case 0x68323634:
    return avcodec_find_decoder(AV_CODEC_ID_H264);
  case 0x68323635:
    return avcodec_find_decoder(AV_CODEC_ID_H265);
  case 0x617631:
    return avcodec_find_decoder(AV_CODEC_ID_AV1);
  default:
    break;
  }

  return NULL;
}

static int read_video_packet(void) {
  unsigned char header[12];
  if (read(STDIN_FILENO, header, sizeof(header)) != sizeof(header)) {
    return 0;
  }

  uint32_t size;
  memcpy(&size, header + 8, sizeof(size));
  size = ntohl(size);

  video_packet = malloc(size);
  if (!video_packet) {
    return 0;
  }

  unsigned char *data = video_packet;
  int len = size;

  while (len > 0) {
    int r = read(STDIN_FILENO, data, len);
    if (r < 0) {
      return 0;
    }
    data += r;
    len -= r;
  }

  return size;
}

static bool init(char **argv) {
  const AVCodec *codec = get_decoder(argv);
  if (!codec) {
    return false;
  }

  parser = av_parser_init((int)codec->id);
  if (!parser) {
    return false;
  }

  codec_ctx = avcodec_alloc_context3(codec);
  if (!codec_ctx) {
    return false;
  }

  if (avcodec_open2(codec_ctx, codec, NULL) < 0) {
    return false;
  }

  frame = av_frame_alloc();
  if (!frame) {
    return false;
  }

  packet = av_packet_alloc();
  if (!packet) {
    return false;
  }

  if (atoi(argv[2])) {
    raw = true;
  }

  if (atoi(argv[3])) {
    alpha = true;
  }

  return true;
}

static void decode_loop(void) {
  for (;;) {
    int len = read_video_packet();
    if (len == 0) {
      return;
    }

    unsigned char *data = video_packet;

    while (len > 0) {
      int r = av_parser_parse2(parser, codec_ctx, &packet->data, &packet->size,
                               data, len, AV_NOPTS_VALUE, AV_NOPTS_VALUE, 0);

      if (r < 0) {
        return;
      }

      data += r;
      len -= r;

      if (packet->size != 0) {
        if (avcodec_send_packet(codec_ctx, packet) < 0) {
          return;
        }

        for (;;) {
          r = avcodec_receive_frame(codec_ctx, frame);
          if (r == AVERROR(EAGAIN) || r == AVERROR_EOF) {
            break;
          }
          if (r < 0) {
            return;
          }

          if (frame_width != frame->width || frame_height != frame->height) {
            frame_width = frame->width;
            frame_height = frame->height;

            if (frame_data) {
              free(frame_data);
            } else if (raw) {
              initial_width = frame_width;
              initial_height = frame_height;
            }

            frame_size = frame_width * frame_height * (alpha ? 4 : 3);
            frame_data = malloc(frame_size);

            if (!frame_data) {
              return;
            }
          }

          if (!raw || (frame->width == initial_width &&
                       frame->height == initial_height)) {
            sws_ctx = sws_getCachedContext(
                sws_ctx, frame_width, frame_height, codec_ctx->pix_fmt,
                frame_width, frame_height,
                alpha ? AV_PIX_FMT_RGBA : AV_PIX_FMT_RGB24, SWS_FAST_BILINEAR,
                NULL, NULL, NULL);

            if (!sws_ctx) {
              return;
            }

            int stride = (alpha ? 4 : 3) * frame_width;

            sws_scale(sws_ctx, (const uint8_t *const *)frame->data,
                      frame->linesize, 0, frame_height, &frame_data, &stride);

            if (!raw) {
              write(STDOUT_FILENO, &frame_width, sizeof(frame_width));
              write(STDOUT_FILENO, &frame_height, sizeof(frame_height));
            }

            write(STDOUT_FILENO, frame_data, frame_size);
          }

          av_frame_unref(frame);
          av_packet_unref(packet);
        }
      }
    }

    free(video_packet);
    video_packet = NULL;
  }
}

int main(int argc, char **argv) {
  if (argc != 4) {
    return 0;
  }

  if (init(argv)) {
    decode_loop();
  }

  if (video_packet) {
    free(video_packet);
  }

  if (frame_data) {
    free(frame_data);
  }

  av_parser_close(parser);
  sws_freeContext(sws_ctx);
  avcodec_free_context(&codec_ctx);
  av_frame_free(&frame);
  av_packet_free(&packet);

  return 0;
}
