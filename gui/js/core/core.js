angular.module('syncthingfuse.core').controller('SyncthingFuseController', function ($scope, $http) {
    $scope.config = {};

    $http.get('/api/system/config').success(function(data) {
        $scope.config = data;
    });

    $scope.thisDevice = function () {
        for (var i = 0; i < $scope.config.devices.length; i++) {
            var device = $scope.config.devices[i];
            if (device.deviceID === $scope.config.myID) {
                return device;
            }
        }
    };

    $scope.otherDevices = function() {
        var devices = [];

        for (var i=0 ; i<$scope.config.devices.length ; i++) {
            device = $scope.config.devices[i];
            if (device.deviceID !== $scope.config.myID) {
                devices.push(device)
            }
        }

        return devices;
    };

    $scope.editFolder = function(folderCfg) {
        $scope.currentFolder = angular.copy(folderCfg);
        $scope.editingExisting = true;
        $scope.folderEditor.$setPristine();
        $('#editFolder').modal();
    };

    $scope.saveFolder = function() {
        $('#editFolder').modal('hide');
        var folderCfg = $scope.currentFolder;

        var folders = [];
        for (var i=0 ; i<$scope.config.folders.length ; i++) {
            if ($scope.config.folders[i].id === folderCfg.id) {
                folders.push(folderCfg)
            } else {
                folders.push($scope.config.folders[i])
            }
        }
        $scope.config.folders = folders;

        $scope.saveConfig();
    };

    $scope.saveConfig = function () {
        var cfg = JSON.stringify($scope.config);
        var opts = {
            headers: {
                'Content-Type': 'application/json'
            }
        };
        $http.post('/api/system/config', cfg, opts).success(function () {
            console.log("saved config successfully"); // TODO show real message
        }).error($scope.emitHTTPError);
    };

    $scope.emitHTTPError = function (data, status, headers, config) {
        // TODO handle errors for serious
        $scope.$emit('HTTPError', {data: data, status: status, headers: headers, config: config});
    };
});
