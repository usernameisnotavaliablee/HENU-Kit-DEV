/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { LearningReportLesson } from './LearningReportLesson';
export type LearningReportAction = {
    kind: 'practice' | 'diagnostic' | 'content_unavailable' | 'no_action';
    reason: string;
    tag_id?: string;
    lesson?: LearningReportLesson;
    question_ids?: Array<string>;
};
