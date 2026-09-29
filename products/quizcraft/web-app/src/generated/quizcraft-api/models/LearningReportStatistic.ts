/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type LearningReportStatistic = {
    tag_id: string;
    tag_kind: 'knowledge' | 'ability';
    label: string;
    attempt_count: number;
    unique_question_count: number;
    first_correct_count: number;
    repeat_attempt_count: number;
    repeat_correct_count: number;
    latest_correct_count: number;
};
