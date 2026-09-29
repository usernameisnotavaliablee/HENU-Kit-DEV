/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type LearningReportFinding = {
    tag_id: string;
    status: 'supported' | 'tentative' | 'uncovered';
    observation: string;
    possible_reason?: string;
    evidence_ids: Array<string>;
};
