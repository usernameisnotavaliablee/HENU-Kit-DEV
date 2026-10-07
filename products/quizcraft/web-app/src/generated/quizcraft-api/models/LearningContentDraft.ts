/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { LearningContentLesson } from './LearningContentLesson';
import type { LearningContentQuestion } from './LearningContentQuestion';
import type { LearningContentSource } from './LearningContentSource';
import type { LearningContentTag } from './LearningContentTag';
export type LearningContentDraft = {
    schema_version: 1;
    tags: Array<LearningContentTag>;
    questions: Array<LearningContentQuestion>;
    sources?: Array<LearningContentSource>;
    lessons?: Array<LearningContentLesson>;
};
