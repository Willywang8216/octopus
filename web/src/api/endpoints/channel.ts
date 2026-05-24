import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';
import { formatCount, formatMoney, formatTime } from '@/lib/utils';
import { StatsChannel, type StatsMetricsFormatted } from './stats';

/**
 * 渠道健康度
 */
export type ChannelHealth = 'alive' | 'flaky' | 'zombie' | 'dead' | 'unknown';

export type ChannelHealthSummary = {
    total: number;
    ok: number;
    failed: number;
    last_tested_at: number;
    health: ChannelHealth;
};

export type ChannelTestProgress = {
    channel_id: number;
    channel_name: string;
    running: boolean;
    phase: 'waiting' | 'running' | 'saving' | 'done' | 'failed' | string;
    current_key_id: number;
    current_key: string;
    current_model: string;
    total_keys: number;
    total_models: number;
    total_probes: number;
    completed_probes: number;
    success_count: number;
    fail_count: number;
    started_at: number;
    updated_at: number;
    finished_at: number;
    last_error: string;
};

export type DuplicateInfo = {
    channel_id: number;
    channel_name: string;
    match_type: 'endpoint_and_key' | 'endpoint' | 'key';
};

export type CheckDuplicateRequest = {
    base_urls: BaseUrl[];
    keys: string[];
    exclude_id?: number;
};

export type CombineChannelRequest = {
    target_id: number;
    base_urls: BaseUrl[];
    keys: Array<Pick<ChannelKey, 'enabled' | 'channel_key' | 'remark'>>;
    model?: string;
    custom_model?: string;
    custom_header?: CustomHeader[];
};

/**
 * 渠道类型枚举
 */
export enum ChannelType {
    OpenAIChat = 0,
    OpenAIResponse = 1,
    Anthropic = 2,
    Gemini = 3,
    Volcengine = 4,
    OpenAIEmbedding = 5,
}

/**
 * 自动分组类型枚举
 */
export enum AutoGroupType {
    None = 0,   // 不自动分组
    Fuzzy = 1,  // 模糊匹配
    Exact = 2,  // 准确匹配
    Regex = 3,  // 正则匹配
}

export type BaseUrl = {
    url: string;
    delay: number;
};

export type CustomHeader = {
    header_key: string;
    header_value: string;
};

/**
 * 渠道测试错误分类（与后端 model.ChannelTestErrorClass 对齐）
 */
export type ChannelTestErrorClass =
    | ''
    | 'auth_invalid'
    | 'permission_denied'
    | 'insufficient_quota'
    | 'rate_limited'
    | 'model_not_found'
    | 'bad_request'
    | 'server_error'
    | 'network_error'
    | 'timeout'
    | 'transform_error'
    | 'unsupported_channel'
    | 'other';

export type ChannelKey = {
    id: number;
    channel_id: number;
    enabled: boolean;
    channel_key: string;
    status_code: number;
    last_use_time_stamp: number;
    retry_after: number;
    failure_count: number;
    last_error: string;
    total_cost: number;
    remark: string;
    auto_disabled?: boolean;
    disabled_reason?: string;
    disabled_class?: ChannelTestErrorClass;
    disabled_at?: number;
    last_test_at?: number;
    last_test_success?: number;
    last_test_failed?: number;
};

/**
 * 渠道完整数据（与后端 model.Channel 对齐；数组字段在前端保证为 []）
 */
export type Channel = {
    id: number;
    name: string;
    type: ChannelType;
    enabled: boolean;
    tags?: string[];
    retry_after: number;
    auto_disable_threshold?: number | null;
    auto_disable_retry_hours?: number | null;
    base_urls: BaseUrl[];
    keys: ChannelKey[];
    model: string;
    custom_model: string;
    proxy: boolean;
    auto_sync: boolean;
    auto_group: AutoGroupType;
    custom_header: CustomHeader[];
    param_override?: string | null;
    channel_proxy?: string | null;
    match_regex?: string | null;
    stats: StatsChannel;
    auto_disabled?: boolean;
    disabled_reason?: string;
    disabled_class?: ChannelTestErrorClass;
    disabled_at?: number;
    last_test_at?: number;
    health?: ChannelHealth;
    test_summary?: ChannelHealthSummary | null;
    test_progress?: ChannelTestProgress | null;
};

/**
 * 单条 (channel, key, model) 探测结果
 */
export type ChannelTestModelResult = {
    id: number;
    channel_id: number;
    key_id: number;
    model: string;
    success: boolean;
    status_code: number;
    latency_ms: number;
    error_class: ChannelTestErrorClass;
    error_msg: string;
    tested_at: number;
};

/**
 * 单个 key 在测试运行中的汇总
 */
export type ChannelTestKeySummary = {
    key_id: number;
    key_preview: string;
    remark: string;
    enabled: boolean;
    auto_disabled: boolean;
    disabled_reason: string;
    disabled_class: ChannelTestErrorClass;
    success_count: number;
    fail_count: number;
    models: ChannelTestModelResult[] | null;
};

export type ChannelDisabledTagDetail = {
    auto_disabled: boolean;
    disabled_reason: string;
    disabled_class: ChannelTestErrorClass;
    disabled_at: number;
};

export type ChannelTestSummary = {
    channel_id: number;
    channel_name: string;
    total_keys: number;
    total_models: number;
    total_probes: number;
    success_count: number;
    fail_count: number;
    duration_ms: number;
    keys: ChannelTestKeySummary[] | null;
    tested_at: number;
    disabled?: ChannelDisabledTagDetail | null;
    running?: boolean;
    progress?: ChannelTestProgress | null;
};

export type ChannelTestAllStatus = {
    running: boolean;
    cancelled?: boolean;
    started_at: number;
    finished_at: number;
    total_channels: number;
    completed_channels: number;
    failed_channels: number;
    last_error: string;
};

export type ChannelTestAllResponse = {
    summaries: ChannelTestSummary[] | null;
    skipped: Array<{ channel_id: number; channel_name: string; reason: string }> | null;
    running?: boolean;
    status?: ChannelTestAllStatus;
};

// Internal type: backend may return null for slice fields; normalize to [] in select()
type ChannelServer = Omit<Channel, 'base_urls' | 'custom_header' | 'keys' | 'tags'> & {
    base_urls: BaseUrl[] | null;
    custom_header: CustomHeader[] | null;
    keys: ChannelKey[] | null;
    tags?: string[] | null;
    health?: ChannelHealth;
    test_summary?: ChannelHealthSummary | null;
    test_progress?: ChannelTestProgress | null;
};

/**
 * 创建渠道请求：必填字段 + 可选字段
 */
export type CreateChannelRequest = {
    name: string;
    type: ChannelType;
    enabled?: boolean;
    base_urls: BaseUrl[];
    keys: Array<Pick<ChannelKey, 'enabled' | 'channel_key' | 'remark'>>;
    model: string;
    custom_model?: string;
    proxy?: boolean;
    auto_sync?: boolean;
    auto_group?: AutoGroupType;
    custom_header?: CustomHeader[];
    channel_proxy?: string | null;
    param_override?: string | null;
    match_regex?: string | null;
    auto_disable_threshold?: number | null;
    auto_disable_retry_hours?: number | null;
};

/**
 * 更新渠道请求：id + 可选字段 + keys diff
 */
export type UpdateChannelRequest = {
    id: number;
    name?: string;
    type?: ChannelType;
    enabled?: boolean;
    base_urls?: BaseUrl[];
    model?: string;
    custom_model?: string;
    proxy?: boolean;
    auto_sync?: boolean;
    auto_group?: AutoGroupType;
    custom_header?: CustomHeader[];
    channel_proxy?: string | null;
    param_override?: string | null;
    match_regex?: string | null;
    auto_disable_threshold?: number | null;
    auto_disable_retry_hours?: number | null;
    // keys diff
    keys_to_add?: Array<Pick<ChannelKey, 'enabled' | 'channel_key' | 'remark'>>;
    keys_to_update?: Array<{ id: number; enabled?: boolean; channel_key?: string; remark?: string }>;
    keys_to_delete?: number[];
};

export type FetchModelRequest = {
    type: ChannelType;
    base_urls: BaseUrl[];
    keys: Array<Pick<ChannelKey, 'enabled' | 'channel_key'>>;
    proxy?: boolean;
    channel_proxy?: string | null;
    match_regex?: string | null;
    custom_header?: CustomHeader[];
};

/**
 * 获取渠道列表 Hook
 * 
 * @example
 * const { data: channels, isLoading, error } = useChannelList();
 * 
 * if (isLoading) return <Loading />;
 * if (error) return <Error message={error.message} />;
 * 
 * channels?.forEach(channel => console.log(channel.raw.name));
 */
export function useChannelList() {
    return useQuery({
        queryKey: ['channels', 'list'],
        queryFn: async () => {
            return apiClient.get<ChannelServer[]>('/api/v1/channel/list');
        },
        select: (data) => data.map((item) => ({
            raw: ({
                ...item,
                base_urls: item.base_urls ?? [],
                custom_header: item.custom_header ?? [],
                keys: item.keys ?? [],
                tags: item.tags ?? [],
                health: item.health ?? 'unknown',
                test_summary: item.test_summary ?? null,
                test_progress: item.test_progress ?? null,
            }) satisfies Channel,
            formatted: {
                input_token: formatCount(item.stats.input_token),
                output_token: formatCount(item.stats.output_token),
                total_token: formatCount(item.stats.input_token + item.stats.output_token),
                input_cost: formatMoney(item.stats.input_cost),
                output_cost: formatMoney(item.stats.output_cost),
                total_cost: formatMoney(item.stats.input_cost + item.stats.output_cost),
                request_success: formatCount(item.stats.request_success),
                request_failed: formatCount(item.stats.request_failed),
                request_count: formatCount(item.stats.request_success + item.stats.request_failed),
                wait_time: formatTime(item.stats.wait_time),
            }
        })) as Array<{ raw: Channel; formatted: StatsMetricsFormatted }>,
        refetchInterval: (query) => {
            const data = query.state.data as Array<ChannelServer | { raw?: Channel }> | undefined;
            const hasRunningProbe = data?.some((item) => {
                const channel = 'raw' in item && item.raw ? item.raw : item as ChannelServer;
                return channel.test_progress?.running === true;
            });
            return hasRunningProbe ? 2000 : 30000;
        },
        refetchOnMount: 'always',
    });
}

/**
 * 创建渠道 Hook
 * 
 * @example
 * const createChannel = useCreateChannel();
 * 
 * createChannel.mutate({
 *   name: 'OpenAI',
 *   type: ChannelType.OpenAIChat,
 *   base_urls: [{ url: 'https://api.openai.com', delay: 0 }],
 *   keys: [{ enabled: true, channel_key: 'sk-xxx' }],
 *   model: 'gpt-4',
 * });
 */
export function useCreateChannel() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: CreateChannelRequest) => {
            return apiClient.post<ChannelServer>('/api/v1/channel/create', data);
        },
        onSuccess: (data) => {
            logger.log('渠道创建成功:', data);
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['models', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
        },
        onError: (error) => {
            logger.error('渠道创建失败:', error);
        },
    });
}

/**
 * 更新渠道 Hook
 * 
 * @example
 * const updateChannel = useUpdateChannel();
 * 
 * updateChannel.mutate({
 *   id: 1,
 *   name: 'OpenAI Updated',
 *   type: ChannelType.OpenAIChat,
 *   enabled: true,
 *   base_urls: [{ url: 'https://api.openai.com', delay: 0 }],
 *   keys_to_add: [{ enabled: true, channel_key: 'sk-xxx' }],
 *   model: 'gpt-4-turbo',
 *   proxy: false,
 * });
 */
export function useUpdateChannel() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: UpdateChannelRequest) => {
            return apiClient.post<ChannelServer>('/api/v1/channel/update', data);
        },
        onSuccess: (data) => {
            logger.log('渠道更新成功:', data);
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
        },
        onError: (error) => {
            logger.error('渠道更新失败:', error);
        },
    });
}

/**
 * 删除渠道 Hook
 * 
 * @example
 * const deleteChannel = useDeleteChannel();
 * 
 * deleteChannel.mutate(1); // 删除 ID 为 1 的渠道
 */
export function useDeleteChannel() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (id: number) => {
            return apiClient.delete<null>(`/api/v1/channel/delete/${id}`);
        },
        onSuccess: () => {
            logger.log('渠道删除成功');
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
        },
        onError: (error) => {
            logger.error('渠道删除失败:', error);
        },
    });
}

/**
 * 启用/禁用渠道 Hook
 * 
 * @example
 * const enableChannel = useEnableChannel();
 * 
 * enableChannel.mutate({ id: 1, enabled: true }); // 启用 ID 为 1 的渠道
 * enableChannel.mutate({ id: 1, enabled: false }); // 禁用 ID 为 1 的渠道
 */
export function useEnableChannel() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: { id: number; enabled: boolean }) => {
            return apiClient.post<null>('/api/v1/channel/enable', data);
        },
        onSuccess: () => {
            logger.log('渠道状态更新成功');
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
        },
        onError: (error) => {
            logger.error('渠道状态更新失败:', error);
        },
    });
}

/**
 * 获取渠道模型列表 Hook
 * 
 * @example
 * const fetchModel = useFetchModel();
 * 
 * fetchModel.mutate({
 *   type: ChannelType.OpenAIChat,
 *   base_urls: [{ url: 'https://api.openai.com', delay: 0 }],
 *   keys: [{ enabled: true, channel_key: 'sk-xxx' }],
 *   proxy: false,
 * });
 * 
 * // 在 onSuccess 中获取模型列表
 * fetchModel.data // ['gpt-4', 'gpt-3.5-turbo', ...]
 */
export function useFetchModel() {
    return useMutation({
        mutationFn: async (data: FetchModelRequest) => {
            return apiClient.post<string[]>('/api/v1/channel/fetch-model', data);
        },
        onSuccess: (data) => {
            logger.log('模型列表获取成功:', data);
        },
        onError: (error) => {
            logger.error('模型列表获取失败:', error);
        },
    });
}

/**
 * 检查渠道端点/API Key 是否与已有渠道重复。
 */
export function useCheckDuplicate() {
    return useMutation({
        mutationFn: async (data: CheckDuplicateRequest) => {
            return apiClient.post<DuplicateInfo[]>('/api/v1/channel/check-duplicate', data);
        },
        onError: (error) => {
            logger.error('渠道重复检查失败:', error);
        },
    });
}

/**
 * 合并新渠道草稿到已有重复渠道。
 */
export function useCombineChannel() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: CombineChannelRequest) => {
            return apiClient.post<ChannelServer>('/api/v1/channel/combine', data);
        },
        onSuccess: (data) => {
            logger.log('渠道合并成功:', data);
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
            queryClient.invalidateQueries({ queryKey: ['models', 'list'] });
        },
        onError: (error) => {
            logger.error('渠道合并失败:', error);
        },
    });
}

/**
 * 获取渠道最后同步时间 Hook
 * 
 * @example
 * const lastSyncTime = useLastSyncTime();
 * 
 * if (lastSyncTime) {
 *   console.log('最后同步时间:', new Date(lastSyncTime).toLocaleString());
 * }
 */
export function useLastSyncTime() {
    return useQuery({
        queryKey: ['channels', 'last-sync-time'],
        queryFn: async () => {
            return apiClient.get<string>('/api/v1/channel/last-sync-time');
        },
        refetchInterval: 30000,
    });
}
/**
 * 同步渠道 Hook
 * 
 * @example
 * const syncChannel = useSyncChannel();
 * 
 * syncChannel.mutate();
 */
export function useSyncChannel() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async () => {
            return apiClient.post<null>('/api/v1/channel/sync');
        },
        onSuccess: () => {
            logger.log('渠道同步成功');
            queryClient.invalidateQueries({ queryKey: ['channels', 'last-sync-time'] });
        },
        onError: (error) => {
            logger.error('渠道同步失败:', error);
        },
    });
}

/**
 * 测试单个渠道的所有 (key × model) 组合 Hook
 *
 * @example
 * const testChannel = useTestChannel();
 * testChannel.mutate({ id: 1 }, { onSuccess: (summary) => console.log(summary) });
 */
export function useChannelTestProgress(channelId: number, enabled = true) {
    return useQuery({
        queryKey: ['channels', 'test-progress', channelId],
        queryFn: async () => {
            return apiClient.get<ChannelTestProgress | null>(`/api/v1/channel/test-progress/${channelId}`);
        },
        enabled: enabled && channelId > 0,
        refetchInterval: (query) => query.state.data?.running ? 1500 : 5000,
        refetchOnMount: 'always',
    });
}

export function useTestChannel() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: { id: number; models?: string[]; include_disabled_keys?: boolean }) => {
            return apiClient.post<ChannelTestSummary>('/api/v1/channel/test', data);
        },
        onSuccess: (data) => {
            logger.log(data.running ? '渠道测试已开始:' : '渠道测试完成:', data);
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['channels', 'test-results', data.channel_id] });
            queryClient.invalidateQueries({ queryKey: ['channels', 'test-progress', data.channel_id] });
            if (data.running) {
                window.setTimeout(() => {
                    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
                    queryClient.invalidateQueries({ queryKey: ['channels', 'test-results', data.channel_id] });
                    queryClient.invalidateQueries({ queryKey: ['channels', 'test-progress', data.channel_id] });
                }, 15000);
            }
        },
        onError: (error) => {
            logger.error('渠道测试失败:', error);
        },
    });
}

/**
 * 一键测试所有渠道的所有 (key × model) 组合 Hook
 */
export function useTestAllChannels() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data?: { include_disabled_keys?: boolean; include_disabled_channels?: boolean }) => {
            return apiClient.post<ChannelTestAllResponse>('/api/v1/channel/test-all', data ?? {});
        },
        onSuccess: (data) => {
            logger.log(data.running ? '全量渠道测试已开始' : '全量渠道测试完成');
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['channels', 'test-results'] });
            if (data.running) {
                window.setTimeout(() => {
                    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
                    queryClient.invalidateQueries({ queryKey: ['channels', 'test-results'] });
                }, 30000);
            }
        },
        onError: (error) => {
            logger.error('全量渠道测试失败:', error);
        },
    });
}


/**
 * Poll the current background test-all job status.
 */
export function useChannelTestAllStatus(enabled = true) {
    return useQuery({
        queryKey: ['channels', 'test-all-status'],
        queryFn: async () => apiClient.get<ChannelTestAllStatus>('/api/v1/channel/test-all-status'),
        enabled,
        refetchInterval: enabled ? 5000 : false,
    });
}

/**
 * 取消渠道测试 Hook
 */
export function useCancelChannelTest() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data?: { channel_id?: number }) => {
            return apiClient.post<{ cancelled: boolean; channel_id?: number }>(
                '/api/v1/channel/cancel-test',
                data ?? {}
            );
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
            queryClient.invalidateQueries({ queryKey: ['channels', 'test-all-status'] });
            queryClient.invalidateQueries({ queryKey: ['channels', 'test-progress'] });
        },
    });
}

/**
 * 获取渠道已缓存的测试结果（不触发新探测）Hook
 */
export function useChannelTestResults(channelId: number | undefined, enabled = true) {
    return useQuery({
        queryKey: ['channels', 'test-results', channelId],
        queryFn: async () => {
            if (!channelId) return null;
            return apiClient.get<ChannelTestSummary>(`/api/v1/channel/test-results/${channelId}`);
        },
        enabled: enabled && typeof channelId === 'number' && channelId > 0,
        staleTime: 10000,
    });
}
