/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type LearningReportPreferences = {
    enabled: boolean;
    interval_days: number;
    goal: 'follow_course' | 'exam_review';
    chapter_ids: Array<string>;
    external_analysis_consent: boolean;
    bank_id: string;
    revision: number;
    next_due_at?: string;
    updated_at?: string;
};
