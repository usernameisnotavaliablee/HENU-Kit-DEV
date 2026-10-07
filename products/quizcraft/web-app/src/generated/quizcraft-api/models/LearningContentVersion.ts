/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type LearningContentVersion = {
    content_version_id: string;
    bank_id: string;
    bank_version_id: string;
    content_sha256: string;
    status: 'draft' | 'approved' | 'retired';
    created_at: string;
    reviewed_by?: string | null;
    reviewed_at?: string | null;
    active: boolean;
    question_count: number;
    lesson_count: number;
    catalog_enabled?: boolean;
};
