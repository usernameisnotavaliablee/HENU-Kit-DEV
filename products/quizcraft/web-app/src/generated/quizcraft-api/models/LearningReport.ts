/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { LearningReportAction } from './LearningReportAction';
import type { LearningReportEvidence } from './LearningReportEvidence';
import type { LearningReportFinding } from './LearningReportFinding';
import type { LearningReportStatistic } from './LearningReportStatistic';
export type LearningReport = {
    report_id: string;
    bank_id: string;
    content_version_id: string;
    status: 'ready' | 'insufficient_evidence' | 'stale';
    goal: 'follow_course' | 'exam_review';
    evidence_until: string;
    created_at: string;
    statistics: Array<LearningReportStatistic>;
    evidence: Array<LearningReportEvidence>;
    findings: Array<LearningReportFinding>;
    next_step: LearningReportAction;
};
