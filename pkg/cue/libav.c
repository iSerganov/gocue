#include <math.h>
#include <stdlib.h>
#include <string.h>
#include <libavcodec/avcodec.h>
#include <libavfilter/avfilter.h>
#include <libavfilter/buffersink.h>
#include <libavfilter/buffersrc.h>
#include <libavutil/bprint.h>
#include <libavutil/channel_layout.h>
#include <libavutil/time.h>
#include "libav.h"

static int deadline_passed(void *opaque) {
    return av_gettime_relative() > ((gocue_input *)opaque)->deadline;
}

gocue_input *gocue_open_input(const char *path, int64_t timeout_us, int *err) {
    gocue_input *in = calloc(1, sizeof(*in));
    if (!in) {
        *err = AVERROR(ENOMEM);
        return NULL;
    }
    in->deadline = av_gettime_relative() + timeout_us;
    in->fmt = avformat_alloc_context();
    if (!in->fmt) {
        free(in);
        *err = AVERROR(ENOMEM);
        return NULL;
    }
    in->fmt->interrupt_callback.callback = deadline_passed;
    in->fmt->interrupt_callback.opaque = in;

    *err = avformat_open_input(&in->fmt, path, NULL, NULL);
    if (*err >= 0)
        *err = avformat_find_stream_info(in->fmt, NULL);
    if (*err < 0) {
        gocue_close_input(in);
        return NULL;
    }
    return in;
}

void gocue_close_input(gocue_input *in) {
    avformat_close_input(&in->fmt);
    free(in);
}

/* Reports whether the whole of s parses as a number. */
static int parse_double(const char *s, double *out) {
    char *end;
    *out = strtod(s, &end);
    return end != s && *end == '\0';
}

static int meta_double(const AVDictionary *meta, const char *key, double *out) {
    const AVDictionaryEntry *e = av_dict_get(meta, key, NULL, AV_DICT_MATCH_CASE);
    return e && parse_double(e->value, out);
}

static int record(gocue_scan_result *res, const AVFrame *frame) {
    const AVDictionaryEntry *e = NULL;
    double v;

    /* ametadata=print skips frames without metadata, such as ebur128's
     * final chunk when it is shorter than 100ms; upstream never sees them */
    if (!av_dict_count(frame->metadata))
        return 0;
    if (res->n == res->cap) {
        size_t cap = res->cap ? res->cap * 2 : 4096;
        int64_t *pts = realloc(res->pts, cap * sizeof(*pts));
        if (!pts)
            return AVERROR(ENOMEM);
        res->pts = pts;
        double *momentary = realloc(res->momentary, cap * sizeof(*momentary));
        if (!momentary)
            return AVERROR(ENOMEM);
        res->momentary = momentary;
        res->cap = cap;
    }
    res->pts[res->n] = frame->pts;
    res->momentary[res->n] = meta_double(frame->metadata, "lavfi.r128.M", &v) ? v : NAN;
    res->n++;

    /* only the last frame's totals are used; a value that does not parse
     * counts as 0, and a NaN peak stays NaN, as Go's max treats it */
    res->integrated = meta_double(frame->metadata, "lavfi.r128.I", &v) ? v : 0;
    res->loudness_range = meta_double(frame->metadata, "lavfi.r128.LRA", &v) ? v : 0;
    res->true_peak = 0;
    while ((e = av_dict_iterate(frame->metadata, e))) {
        if (strncmp(e->key, "lavfi.r128.true_peaks_ch", 24) || !parse_double(e->value, &v))
            continue;
        res->true_peak = isnan(res->true_peak) || isnan(v) ? NAN : fmax(res->true_peak, v);
    }
    return 0;
}

static int drain(AVFilterContext *sink, AVFrame *out, gocue_scan_result *res) {
    for (;;) {
        int err = av_buffersink_get_frame(sink, out);
        if (err == AVERROR(EAGAIN) || err == AVERROR_EOF)
            return 0;
        if (err < 0)
            return err;
        err = record(res, out);
        av_frame_unref(out);
        if (err < 0)
            return err;
    }
}

static int build_graph(AVFilterGraph *graph, AVFilterContext **src, AVFilterContext **sink,
                       const AVCodecContext *dec, const char *filters) {
    AVFilterInOut *outputs = avfilter_inout_alloc(), *inputs = avfilter_inout_alloc();
    AVBPrint args;
    int err = AVERROR(ENOMEM);

    /* the same abuffer arguments the ffmpeg CLI builds for a decoded stream */
    av_bprint_init(&args, 0, AV_BPRINT_SIZE_AUTOMATIC);
    av_bprintf(&args, "time_base=1/%d:sample_rate=%d:sample_fmt=%s",
               dec->sample_rate, dec->sample_rate, av_get_sample_fmt_name(dec->sample_fmt));
    if (av_channel_layout_check(&dec->ch_layout) && dec->ch_layout.order != AV_CHANNEL_ORDER_UNSPEC) {
        av_bprintf(&args, ":channel_layout=");
        av_channel_layout_describe_bprint(&dec->ch_layout, &args);
    } else
        av_bprintf(&args, ":channels=%d", dec->ch_layout.nb_channels);

    if (!outputs || !inputs)
        goto end;
    if ((err = avfilter_graph_create_filter(src, avfilter_get_by_name("abuffer"), "in", args.str, NULL, graph)) < 0)
        goto end;
    if ((err = avfilter_graph_create_filter(sink, avfilter_get_by_name("abuffersink"), "out", NULL, NULL, graph)) < 0)
        goto end;

    outputs->name = av_strdup("in");
    outputs->filter_ctx = *src;
    inputs->name = av_strdup("out");
    inputs->filter_ctx = *sink;
    if ((err = avfilter_graph_parse_ptr(graph, filters, &inputs, &outputs, NULL)) < 0)
        goto end;
    err = avfilter_graph_config(graph, NULL);
end:
    avfilter_inout_free(&inputs);
    avfilter_inout_free(&outputs);
    return err;
}

typedef struct {
    AVCodecContext *dec;
    AVFilterContext *src, *sink;
    AVFrame *decoded, *filtered;
    AVRational stream_tb;
    int64_t start, next_pts;
} pipeline;

/* Sends one packet (NULL flushes) through the decoder and the filter graph.
 * Like the ffmpeg CLI, a corrupt packet is skipped rather than ending the
 * scan, and timestamps start at zero relative to the file's start time. */
static int decode(pipeline *p, const AVPacket *pkt, gocue_scan_result *res) {
    int err = avcodec_send_packet(p->dec, pkt);
    if (err == AVERROR_INVALIDDATA)
        return 0;
    if (err < 0)
        return err;
    for (;;) {
        err = avcodec_receive_frame(p->dec, p->decoded);
        if (err == AVERROR(EAGAIN) || err == AVERROR_EOF || err == AVERROR_INVALIDDATA)
            return 0;
        if (err < 0)
            return err;
        AVRational tb = {1, p->decoded->sample_rate};
        int64_t ts = p->decoded->best_effort_timestamp;
        p->decoded->pts = ts == AV_NOPTS_VALUE ? p->next_pts : av_rescale_q(ts - p->start, p->stream_tb, tb);
        p->next_pts = p->decoded->pts + p->decoded->nb_samples;
        if ((err = av_buffersrc_add_frame(p->src, p->decoded)) < 0)
            return err;
        if ((err = drain(p->sink, p->filtered, res)) < 0)
            return err;
    }
}

gocue_scan_result *gocue_scan(const char *path, const char *filters, int64_t timeout_us, int *err) {
    gocue_input *in = gocue_open_input(path, timeout_us, err);
    gocue_scan_result *res = calloc(1, sizeof(*res));
    AVFilterGraph *graph = avfilter_graph_alloc();
    AVPacket *pkt = av_packet_alloc();
    pipeline p = {.decoded = av_frame_alloc(), .filtered = av_frame_alloc()};
    const AVCodec *codec;
    AVStream *st;
    int idx;

    if (!in)
        goto end;
    if (!res || !graph || !pkt || !p.decoded || !p.filtered) {
        *err = AVERROR(ENOMEM);
        goto end;
    }

    if ((idx = *err = av_find_best_stream(in->fmt, AVMEDIA_TYPE_AUDIO, -1, -1, &codec, 0)) < 0)
        goto end;
    st = in->fmt->streams[idx];
    p.stream_tb = st->time_base;
    p.start = in->fmt->start_time == AV_NOPTS_VALUE ? 0
              : av_rescale_q(in->fmt->start_time, AV_TIME_BASE_Q, p.stream_tb);
    if (!(p.dec = avcodec_alloc_context3(codec))) {
        *err = AVERROR(ENOMEM);
        goto end;
    }
    if ((*err = avcodec_parameters_to_context(p.dec, st->codecpar)) < 0)
        goto end;
    p.dec->pkt_timebase = p.stream_tb;
    if ((*err = avcodec_open2(p.dec, codec, NULL)) < 0)
        goto end;
    if ((*err = build_graph(graph, &p.src, &p.sink, p.dec, filters)) < 0)
        goto end;
    res->time_base = av_buffersink_get_time_base(p.sink);

    while ((*err = av_read_frame(in->fmt, pkt)) >= 0) {
        if (deadline_passed(in))
            *err = AVERROR_EXIT;
        else if (pkt->stream_index == idx)
            *err = decode(&p, pkt, res);
        av_packet_unref(pkt);
        if (*err < 0)
            goto end;
    }
    if (*err != AVERROR_EOF)
        goto end;
    if ((*err = decode(&p, NULL, res)) < 0)
        goto end;
    if ((*err = av_buffersrc_add_frame(p.src, NULL)) < 0)
        goto end;
    *err = drain(p.sink, p.filtered, res);
end:
    av_frame_free(&p.decoded);
    av_frame_free(&p.filtered);
    av_packet_free(&pkt);
    avcodec_free_context(&p.dec);
    avfilter_graph_free(&graph);
    if (in)
        gocue_close_input(in);
    if (*err < 0 && res) {
        gocue_scan_free(res);
        return NULL;
    }
    return res;
}

void gocue_scan_free(gocue_scan_result *res) {
    free(res->pts);
    free(res->momentary);
    free(res);
}
