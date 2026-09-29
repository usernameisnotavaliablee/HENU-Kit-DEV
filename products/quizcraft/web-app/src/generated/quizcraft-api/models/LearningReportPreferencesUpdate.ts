/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type LearningReportPreferencesUpdate = {
    enabled: boolean;
    interval_days: number;
    goal: 'follow_course' | 'exam_review';
    chapter_ids: Array<string>;
    external_analysis_consent: boolean;
};
