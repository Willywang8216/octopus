'use client';

import { useState } from 'react';
import { CheckCircle2, ChevronDown, ChevronRight, FlaskConical, Loader2, XCircle } from 'lucide-react';
import {
    useChannelTestResult,
    useStartChannelTest,
    type ChannelTestKeyResult,
    type ChannelTestModelResult,
} from '@/api/endpoints/channel';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';

/**
 * TestResults shows the per-key, per-model outcome for a single channel.
 *
 * Designed as a drop-in section for the channel detail dialog. It lazily
 * fetches the per-channel report (the channel list view only carries
 * lightweight summaries) and offers a "Re-test this channel" CTA that
 * re-runs the global test scoped to just this channel id.
 */
export function TestResults({ channelID }: { channelID: number }) {
    const { data: result, isLoading } = useChannelTestResult(channelID, true);
    const startTest = useStartChannelTest();

    const handleReTest = (e: React.MouseEvent<HTMLButtonElement>) => {
        e.stopPropagation();
        e.preventDefault();
        startTest.mutate([channelID], {
            onSuccess: () => toast.success('Re-testing this channel'),
            onError: (err) => {
                const msg = err && typeof err === 'object' && 'message' in err && typeof err.message === 'string'
                    ? err.message
                    : 'Failed to start test';
                toast.error(msg);
            },
        });
    };

    return (
        <section className="space-y-3">
            <div className="flex items-center justify-between">
                <h4 className="flex items-center gap-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    <FlaskConical className="size-3.5" />
                    Test results
                </h4>
                <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={handleReTest}
                    disabled={startTest.isPending}
                    className="rounded-xl h-7 px-2 text-xs"
                >
                    {startTest.isPending ? (
                        <>
                            <Loader2 className="size-3 animate-spin" />
                            Starting…
                        </>
                    ) : (
                        <>
                            <FlaskConical className="size-3" />
                            Re-test
                        </>
                    )}
                </Button>
            </div>

            {isLoading && !result && (
                <div className="rounded-2xl border bg-card p-4 text-sm text-muted-foreground">
                    <Loader2 className="mr-2 inline size-4 animate-spin" />
                    Loading test results…
                </div>
            )}

            {!isLoading && !result && (
                <div className="rounded-2xl border bg-card p-4 text-sm text-muted-foreground">
                    This channel has not been tested yet. Click <strong>Test all channels</strong> on the channel
                    page or use <strong>Re-test</strong> above.
                </div>
            )}

            {result && (
                <div className="space-y-3">
                    {result.skipped && (
                        <div className="rounded-2xl border border-muted-foreground/30 bg-muted/40 p-3 text-sm text-muted-foreground">
                            <strong className="mr-1">Skipped:</strong>
                            {result.skipped}
                        </div>
                    )}

                    <dl className="grid grid-cols-2 gap-2">
                        <Stat label="Models OK" value={`${result.worked_models}/${result.total_models}`} />
                        <Stat label="Keys OK" value={`${result.worked_keys}/${result.total_keys}`} />
                    </dl>

                    {result.keys.length === 0 && !result.skipped && (
                        <div className="rounded-2xl border bg-card p-4 text-sm text-muted-foreground">
                            No probe results recorded. Run a test to populate this view.
                        </div>
                    )}

                    {result.keys.map((kr) => (
                        <KeyBlock key={kr.key_id} keyResult={kr} />
                    ))}
                </div>
            )}
        </section>
    );
}

function Stat({ label, value }: { label: string; value: string }) {
    return (
        <div className="rounded-2xl border bg-card p-3">
            <dt className="text-[11px] text-muted-foreground">{label}</dt>
            <dd className="text-base font-semibold">{value}</dd>
        </div>
    );
}

function KeyBlock({ keyResult }: { keyResult: ChannelTestKeyResult }) {
    const successCount = keyResult.results.filter((r) => r.success).length;
    const totalCount = keyResult.results.length;
    const allOk = totalCount > 0 && successCount === totalCount;
    const allFail = totalCount > 0 && successCount === 0;

    const [expanded, setExpanded] = useState(allFail || (!allOk && totalCount > 0));

    const toneClass = allOk
        ? 'border-emerald-500/40 bg-emerald-500/5'
        : allFail
            ? 'border-red-500/40 bg-red-500/5'
            : 'border-orange-500/40 bg-orange-500/5';

    return (
        <div className={cn('rounded-2xl border bg-card overflow-hidden', toneClass)}>
            <button
                type="button"
                onClick={() => setExpanded((v) => !v)}
                className="flex w-full items-center justify-between gap-3 p-3 text-left hover:bg-accent/5 transition-colors"
            >
                <div className="flex items-center gap-2 min-w-0">
                    {expanded ? (
                        <ChevronDown className="size-4 text-muted-foreground shrink-0" />
                    ) : (
                        <ChevronRight className="size-4 text-muted-foreground shrink-0" />
                    )}
                    <span className="font-mono text-sm truncate">{keyResult.key_preview}</span>
                    {keyResult.key_remark && (
                        <span className="text-xs text-muted-foreground truncate">{keyResult.key_remark}</span>
                    )}
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    <Badge
                        variant="secondary"
                        className={cn(
                            'h-5 px-1.5 text-[10px]',
                            allOk
                                ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                                : allFail
                                    ? 'bg-red-500/15 text-red-700 dark:text-red-400'
                                    : 'bg-orange-500/15 text-orange-700 dark:text-orange-400',
                        )}
                    >
                        {successCount}/{totalCount} OK
                    </Badge>
                </div>
            </button>

            {expanded && (
                <ul className="divide-y border-t">
                    {keyResult.results.map((mr) => (
                        <ModelRow key={mr.model} result={mr} />
                    ))}
                    {keyResult.results.length === 0 && (
                        <li className="p-3 text-xs text-muted-foreground">No models tested for this key.</li>
                    )}
                </ul>
            )}
        </div>
    );
}

function ModelRow({ result }: { result: ChannelTestModelResult }) {
    return (
        <li className="flex flex-col gap-1 p-3 sm:flex-row sm:items-center sm:gap-3">
            <span className="flex shrink-0 items-center gap-2">
                {result.success ? (
                    <CheckCircle2 className="size-4 text-emerald-500" />
                ) : (
                    <XCircle className="size-4 text-red-500" />
                )}
            </span>
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-baseline gap-2">
                    <span className="font-mono text-sm truncate">{result.model}</span>
                    {result.status_code > 0 && (
                        <Badge
                            variant="secondary"
                            className={cn(
                                'h-4 px-1 text-[10px]',
                                result.status_code >= 200 && result.status_code < 300
                                    ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                                    : 'bg-red-500/15 text-red-700 dark:text-red-400',
                            )}
                        >
                            HTTP {result.status_code}
                        </Badge>
                    )}
                    {result.duration_ms > 0 && (
                        <span className="text-[11px] text-muted-foreground">{result.duration_ms}ms</span>
                    )}
                </div>
                {!result.success && result.error && (
                    <p className="mt-1 break-words text-[11px] text-muted-foreground">{result.error}</p>
                )}
            </div>
        </li>
    );
}
