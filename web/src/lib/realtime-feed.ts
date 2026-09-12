import type { Exchange, ExchangeDetail, ExchangeSummary } from '$lib/api/types';
import { mergeNewest } from '$lib/history';

export const realtimeExchangeLimit = 50;

export interface RealtimeExchange {
  summary: ExchangeSummary;
  detail?: ExchangeDetail;
  error: string;
  unavailable: boolean;
}

export function summaryFromExchange(exchange: Exchange): ExchangeSummary {
	if (exchange.transport === 'udp') {
		return {
			id: exchange.id,
			transport: 'udp',
			mode: exchange.mode,
			state: exchange.state,
			revision: exchange.revision,
			config_revision: exchange.config_revision,
			started_at: exchange.started_at,
			completed_at: exchange.completed_at,
			source_address: exchange.datagram.source_address,
			local_address: exchange.datagram.local_address,
			datagram_bytes: exchange.datagram.accepted_bytes,
			discard_reason: exchange.datagram.discard_reason,
			duration_ns: exchange.duration_ns,
			error: exchange.error
		};
	}
	return {
    id: exchange.id,
    mode: exchange.mode,
    state: exchange.state,
    revision: exchange.revision,
    config_revision: exchange.config_revision,
    started_at: exchange.started_at,
    completed_at: exchange.completed_at,
    method: exchange.request.method,
    path: exchange.request.path,
    request_body_bytes: exchange.request.observed_body_bytes,
    response_body_bytes: exchange.response?.observed_body_bytes ?? 0,
    response_status: exchange.response?.status,
    duration_ns: exchange.duration_ns,
    request_preview: exchange.request.preview,
    preview_truncated: exchange.request.preview_truncated,
    error: exchange.error
  };
}

export function mergeRealtime(current: RealtimeExchange[], incoming: ExchangeSummary[]): RealtimeExchange[] {
  const currentByID = new Map(current.map((item) => [item.summary.id, item]));
  const summaries = mergeNewest([], [...current.map((item) => item.summary), ...incoming]);
  return summaries.slice(0, realtimeExchangeLimit).map((summary) => {
    const existing = currentByID.get(summary.id);
    if (existing?.summary.revision === summary.revision) return { ...existing, summary };
    return { summary, error: '', unavailable: false };
  });
}
