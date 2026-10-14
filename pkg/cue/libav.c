#include <math.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <libavcodec/avcodec.h>
#include <libavfilter/avfilter.h>
#include <libavfilter/buffersink.h>
#include <libavfilter/buffersrc.h>
#include <libavutil/bprint.h>
#include <libavutil/channel_layout.h>
#include <libavutil/threadmessage.h>
#include <libavutil/time.h>
#include "libav.h"

/* how many decoded frames may wait for each true-peak thread */
#define PEAK_QUEUE_FRAMES 64

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

/* A filter graph fed with decoded frames, whose output frames go to on_frame. */
typedef struct {
    AVFilterGraph *graph;
    AVFilterContext *src, *sink;
    AVFrame *out;
    int (*on_frame)(void *ctx, const AVFrame *frame);
    void *ctx;
} chain;

static int chain_open(chain *c, const AVCodecContext *dec, const char *filters,
                      int (*on_frame)(void *, const AVFrame *), void *ctx) {
    c->on_frame = on_frame;
    c->ctx = ctx;
    if (!(c->graph = avfilter_graph_alloc()) || !(c->out = av_frame_alloc()))
        return AVERROR(ENOMEM);
    return build_graph(c->graph, &c->src, &c->sink, dec, filters);
}

/* Feeds frame, or the end of the stream when frame is NULL, and hands every
 * frame the graph produces to on_frame. The graph takes over frame's data. */
static int chain_feed(chain *c, AVFrame *frame) {
    int err = av_buffersrc_add_frame(c->src, frame);
    while (err >= 0) {
        err = av_buffersink_get_frame(c->sink, c->out);
        if (err == AVERROR(EAGAIN) || err == AVERROR_EOF)
            return 0;
        if (err >= 0) {
            err = c->on_frame(c->ctx, c->out);
            av_frame_unref(c->out);
        }
    }
    return err;
}

static void chain_close(chain *c) {
    av_frame_free(&c->out);
    avfilter_graph_free(&c->graph);
}

static int record(void *ctx, const AVFrame *frame) {
    gocue_scan_result *res = ctx;
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
     * counts as 0 */
    res->integrated = meta_double(frame->metadata, "lavfi.r128.I", &v) ? v : 0;
    res->loudness_range = meta_double(frame->metadata, "lavfi.r128.LRA", &v) ? v : 0;
    return 0;
}

/* Measures one channel's true peak on its own thread. Oversampling for the
 * true peak is most of the scan's cost, and it is the same per channel
 * whether measured alone or with the others. */
typedef struct {
    chain chain;
    AVThreadMessageQueue *queue;
    pthread_t thread;
    int running, err, has_peak;
    double peak;
} peak_worker;

static int record_peak(void *ctx, const AVFrame *frame) {
    peak_worker *w = ctx;
    /* keep the peak of the last frame that record would also keep */
    if (av_dict_count(frame->metadata))
        w->has_peak = meta_double(frame->metadata, "lavfi.r128.true_peaks_ch0", &w->peak);
    return 0;
}

static void free_frame_message(void *msg) {
    av_frame_free(msg);
}

static void *run_peak_worker(void *arg) {
    peak_worker *w = arg;
    AVFrame *frame;
    int err;

    while ((err = av_thread_message_queue_recv(w->queue, &frame, 0)) >= 0) {
        err = chain_feed(&w->chain, frame);
        av_frame_free(&frame);
        if (err < 0)
            break;
    }
    if (err == AVERROR_EOF)
        err = chain_feed(&w->chain, NULL);
    if (err < 0)
        av_thread_message_queue_set_err_send(w->queue, err);
    w->err = err;
    return NULL;
}

static int start_peak_worker(peak_worker *w, const AVCodecContext *dec, int channel, double target) {
    char filters[128];
    int err;

    snprintf(filters, sizeof(filters), "pan=mono|c0=c%d,ebur128=target=%.3f:peak=true:metadata=1", channel, target);
    if ((err = chain_open(&w->chain, dec, filters, record_peak, w)) < 0)
        return err;
    if ((err = av_thread_message_queue_alloc(&w->queue, PEAK_QUEUE_FRAMES, sizeof(AVFrame *))) < 0)
        return err;
    av_thread_message_queue_set_free_func(w->queue, free_frame_message);
    if ((err = pthread_create(&w->thread, NULL, run_peak_worker, w)))
        return AVERROR(err);
    w->running = 1;
    return 0;
}

/* Ends the worker's stream with end: AVERROR_EOF after the last frame, or the
 * error that stopped the scan, in which case waiting frames are dropped. */
static int stop_peak_worker(peak_worker *w, int end) {
    if (w->running) {
        if (end != AVERROR_EOF)
            av_thread_message_flush(w->queue);
        av_thread_message_queue_set_err_recv(w->queue, end);
        pthread_join(w->thread, NULL);
        w->running = 0;
    }
    av_thread_message_queue_free(&w->queue);
    chain_close(&w->chain);
    return w->err;
}

typedef struct {
    AVCodecContext *dec;
    AVFrame *decoded;
    AVRational stream_tb;
    int64_t start, next_pts;
    chain loudness;
    peak_worker *workers;
    int nb_workers;
} pipeline;

/* Sends a decoded frame to every true-peak thread and to the loudness graph. */
static int feed(pipeline *p) {
    for (int i = 0; i < p->nb_workers; i++) {
        AVFrame *copy = av_frame_clone(p->decoded);
        int err;
        if (!copy)
            return AVERROR(ENOMEM);
        if ((err = av_thread_message_queue_send(p->workers[i].queue, &copy, 0)) < 0) {
            av_frame_free(&copy);
            return err;
        }
    }
    return chain_feed(&p->loudness, p->decoded);
}

/* Sends one packet (NULL flushes) through the decoder and on to the graphs.
 * Like the ffmpeg CLI, a corrupt packet is skipped rather than ending the
 * scan, and timestamps start at zero relative to the file's start time. */
static int decode(pipeline *p, const AVPacket *pkt) {
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
        if ((err = feed(p)) < 0)
            return err;
    }
}

gocue_scan_result *gocue_scan(const char *path, double target, int64_t timeout_us, int *err) {
    gocue_input *in = gocue_open_input(path, timeout_us, err);
    gocue_scan_result *res = calloc(1, sizeof(*res));
    AVPacket *pkt = av_packet_alloc();
    pipeline p = {.decoded = av_frame_alloc()};
    char filters[64];
    const AVCodec *codec;
    AVStream *st;
    int idx;

    if (!in)
        goto end;
    if (!res || !pkt || !p.decoded) {
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

    snprintf(filters, sizeof(filters), "ebur128=target=%.3f:metadata=1", target);
    if ((*err = chain_open(&p.loudness, p.dec, filters, record, res)) < 0)
        goto end;
    res->time_base = av_buffersink_get_time_base(p.loudness.sink);
    if (!(p.workers = av_calloc(p.dec->ch_layout.nb_channels, sizeof(*p.workers)))) {
        *err = AVERROR(ENOMEM);
        goto end;
    }
    for (; p.nb_workers < p.dec->ch_layout.nb_channels; p.nb_workers++)
        if ((*err = start_peak_worker(&p.workers[p.nb_workers], p.dec, p.nb_workers, target)) < 0) {
            p.nb_workers++;
            goto end;
        }

    while ((*err = av_read_frame(in->fmt, pkt)) >= 0) {
        if (deadline_passed(in))
            *err = AVERROR_EXIT;
        else if (pkt->stream_index == idx)
            *err = decode(&p, pkt);
        av_packet_unref(pkt);
        if (*err < 0)
            goto end;
    }
    if (*err != AVERROR_EOF)
        goto end;
    if ((*err = decode(&p, NULL)) < 0)
        goto end;
    *err = chain_feed(&p.loudness, NULL);
end:
    for (int i = 0; i < p.nb_workers; i++) {
        int werr = stop_peak_worker(&p.workers[i], *err < 0 ? *err : AVERROR_EOF);
        if (*err >= 0 && werr < 0)
            *err = werr;
    }
    if (*err >= 0) {
        /* a NaN peak stays NaN, as Go's max treats it */
        res->true_peak = 0;
        for (int i = 0; i < p.nb_workers; i++) {
            double v = p.workers[i].peak;
            if (p.workers[i].has_peak)
                res->true_peak = isnan(res->true_peak) || isnan(v) ? NAN : v > res->true_peak ? v : res->true_peak;
        }
    }
    av_free(p.workers);
    chain_close(&p.loudness);
    av_frame_free(&p.decoded);
    av_packet_free(&pkt);
    avcodec_free_context(&p.dec);
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
