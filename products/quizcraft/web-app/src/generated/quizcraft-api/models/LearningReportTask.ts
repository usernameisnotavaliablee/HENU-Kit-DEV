/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type LearningReportTask = {
    task_id: string;
    bank_id: string;
    status: 'queued' | 'running' | 'ready' | 'failed' | 'paused' | 'cancelled';
    created_at: string;
    report_id?: string;
    reason_code?: string;
    retry_after_seconds?: number;
};
