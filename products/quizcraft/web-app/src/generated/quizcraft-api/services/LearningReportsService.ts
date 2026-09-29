/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { LearningReportClearResultEnvelope } from '../models/LearningReportClearResultEnvelope';
import type { LearningReportEnvelope } from '../models/LearningReportEnvelope';
import type { LearningReportPreferencesEnvelope } from '../models/LearningReportPreferencesEnvelope';
import type { LearningReportPreferencesUpdate } from '../models/LearningReportPreferencesUpdate';
import type { LearningReportTaskEnvelope } from '../models/LearningReportTaskEnvelope';
import type { PracticeSessionEnvelope } from '../models/PracticeSessionEnvelope';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class LearningReportsService {
    /**
     * Read course-scoped feedback preferences
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Missing preferences return disabled, seven-day defaults.
     * @returns LearningReportPreferencesEnvelope Owner preferences
     * @throws ApiError
     */
    public static getPortalLearningReportPreferences({
        bankId,
        xActorUserId,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
    }): CancelablePromise<LearningReportPreferencesEnvelope> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports/preferences',
            path: {
                'bank_id': bankId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
            },
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
    /**
     * Replace feedback preferences and consent
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Enabling requires explicit external-analysis consent. Goal is changeable, not a permanent user category. Disabling invalidates queued/in-flight generations. Owners may disable or clear derived data even after membership revocation; cleanup never reads reports or starts model work.
     * @returns LearningReportPreferencesEnvelope Saved preferences
     * @throws ApiError
     */
    public static updatePortalLearningReportPreferences({
        bankId,
        xActorUserId,
        idempotencyKey,
        requestBody,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
        idempotencyKey: string,
        requestBody: LearningReportPreferencesUpdate,
    }): CancelablePromise<LearningReportPreferencesEnvelope> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports/preferences',
            path: {
                'bank_id': bankId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
                'Idempotency-Key': idempotencyKey,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
    /**
     * Request an evidence-based learning report
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Identical valid inputs reuse existing output or task. Manual requests have no daily quota and consume no credits; abuse rate limiting only. Manual requests do not reset the automatic schedule.
     * @returns LearningReportTaskEnvelope Reused result or pending task
     * @throws ApiError
     */
    public static requestPortalLearningReport({
        bankId,
        xActorUserId,
        idempotencyKey,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
        idempotencyKey: string,
    }): CancelablePromise<LearningReportTaskEnvelope> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports',
            path: {
                'bank_id': bankId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
                'Idempotency-Key': idempotencyKey,
            },
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                429: `Abuse rate limit; not a usage quota`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
    /**
     * Clear derived feedback and cancel outstanding work
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Disable consent, increment invalidation revision, clear snapshots/reports and cancel jobs atomically. Late results cannot repopulate data. Preserve original practice attempts. Owners may disable or clear derived data even after membership revocation; cleanup never reads reports or starts model work.
     * @returns LearningReportClearResultEnvelope Derived feedback cleared
     * @throws ApiError
     */
    public static clearPortalLearningReports({
        bankId,
        xActorUserId,
        idempotencyKey,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
        idempotencyKey: string,
    }): CancelablePromise<LearningReportClearResultEnvelope> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports',
            path: {
                'bank_id': bankId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
                'Idempotency-Key': idempotencyKey,
            },
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
    /**
     * Read the latest course report
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Return 404 if absent. Report data remains actor-bound. Stale reports are marked and cannot launch obsolete recommendations.
     * @returns LearningReportEnvelope Latest report
     * @throws ApiError
     */
    public static getPortalLatestLearningReport({
        bankId,
        xActorUserId,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
    }): CancelablePromise<LearningReportEnvelope> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports/latest',
            path: {
                'bank_id': bankId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
            },
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
    /**
     * Read an owner-scoped report task
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Task IDs never grant access across actors or banks.
     * @returns LearningReportTaskEnvelope Task status
     * @throws ApiError
     */
    public static getPortalLearningReportTask({
        bankId,
        xActorUserId,
        taskId,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
        taskId: string,
    }): CancelablePromise<LearningReportTaskEnvelope> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports/tasks/{task_id}',
            path: {
                'bank_id': bankId,
                'task_id': taskId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
            },
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
    /**
     * Practice the verified report recommendation
     * Internal-only, disabled by default. Verified owner and live lifetime entitlement required for generation and report/lesson access; dependency errors fail closed. No guest access. Bank/chapter scope uses published content only. Revalidate publication, approved content version and current access. Use server-selected existing questions; do not accept client question IDs, generate new questions or change scoring.
     * @returns PracticeSessionEnvelope Pinned practice session
     * @throws ApiError
     */
    public static createPortalLearningReportPracticeSession({
        bankId,
        xActorUserId,
        reportId,
        idempotencyKey,
    }: {
        bankId: string,
        /**
         * Verified Portal actor, included in the service HMAC; never accepted from request JSON.
         */
        xActorUserId: string,
        reportId: string,
        idempotencyKey: string,
    }): CancelablePromise<PracticeSessionEnvelope> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/portal/practice/banks/{bank_id}/learning-reports/results/{report_id}/practice-sessions',
            path: {
                'bank_id': bankId,
                'report_id': reportId,
            },
            headers: {
                'X-Actor-User-Id': xActorUserId,
                'Idempotency-Key': idempotencyKey,
            },
            errors: {
                400: `Invalid request`,
                401: `Missing or invalid actor credentials`,
                403: `Permission code or product Scope denied`,
                404: `Resource or operation is unknown to this actor`,
                409: `Idempotency payload or optimistic version conflict`,
                503: `PostgreSQL or a required service is unavailable`,
            },
        });
    }
}
