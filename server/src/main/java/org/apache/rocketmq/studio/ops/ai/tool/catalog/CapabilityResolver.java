/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.apache.rocketmq.studio.ops.ai.tool.catalog;

import lombok.RequiredArgsConstructor;
import org.apache.rocketmq.studio.common.exception.BusinessException;
import org.apache.rocketmq.studio.instance.InstanceResolver;
import org.apache.rocketmq.studio.instance.InstanceVO;
import org.apache.rocketmq.studio.ops.ai.tool.core.ToolError;
import org.springframework.stereotype.Component;

import java.util.HashSet;
import java.util.List;
import java.util.Set;


@Component
@RequiredArgsConstructor
public class CapabilityResolver {

    private final InstanceResolver instanceResolver;

    public Set<String> resolve(String cluster) {
        if (cluster == null || cluster.isBlank()) {
            throw ToolError.CAPABILITY_CLUSTER_REQUIRED.exception();
        }
        InstanceVO instance = instanceResolver.findByName(cluster)
                .orElseThrow(() -> ToolError.INSTANCE_NOT_FOUND.exception(cluster));
        return new HashSet<>(resolve(instance));
    }

    List<String> resolve(InstanceVO instanceVO) {
        if (instanceVO.getType() == null) {
            throw new BusinessException(
                    400, "Cluster type is unavailable: " + instanceVO.getId());
        }
        return switch (instanceVO.getType()) {
            case CLOUD -> List.of(
                    "ACL_V2",
                    "REMOTING");
            case DIRECT -> List.of(
                    "REMOTING",
                    "ROCKETMQ_4");
            case PROXY_LOCAL -> List.of(
                    "ACL_V2",
                    "GRPC",
                    "LITE_TOPIC",
                    "LOCAL_PROXY",
                    "POP",
                    "REMOTING",
                    "ROCKETMQ_5");
            case PROXY_CLUSTER -> List.of(
                    "ACL_V2",
                    "CLUSTER_PROXY",
                    "GRPC",
                    "LITE_TOPIC",
                    "POP",
                    "REMOTING",
                    "ROCKETMQ_5");
        };
    }
}
